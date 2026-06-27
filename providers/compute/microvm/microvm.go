//go:build firecracker

package microvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Provider boots boxes as Firecracker microVMs. F1.1 skeleton: it boots a static
// VM from a golden rootfs (copied per-VM; CoW is F1.4) over the serial console,
// with no network yet (F1.2). It runs firecracker via `--no-api --config-file`,
// the mechanism proven on the host; the API socket (for snapshots/F4) is a later
// upgrade.
type Provider struct {
	fcBin  string      // firecracker binary
	kernel string      // vmlinux
	rootfs string      // golden base rootfs
	runDir string      // per-VM working dirs live here
	net    *vmNet      // host bridge + tap + IP allocation
	pool   *rootfsPool // CoW rootfs clones (dm snapshot, or copy fallback)

	mu  sync.Mutex
	vms map[string]*vm
}

type vm struct {
	cmd       *exec.Cmd
	dir       string
	ip        string      // allocated guest IP
	tap       string      // host tap device
	sock      string      // firecracker API socket (for snapshot/suspend, F4)
	teardown  func()      // release the CoW rootfs clone
	suspended bool        // snapshotted to disk; firecracker not running
	done      atomic.Bool // set when the firecracker process exits
}

var (
	_ ports.Compute   = (*Provider)(nil)
	_ ports.Suspender = (*Provider)(nil)
)

func (v *vm) snapPaths() (state, mem string) {
	return filepath.Join(v.dir, "snapshot"), filepath.Join(v.dir, "mem")
}

// New builds the provider. It requires /dev/kvm and sets up the VM bridge +
// egress fence. allowHostPorts are the host ports a box may reach (agent hub +
// metadata); everything else on the host is blocked.
func New(fcBin, kernel, rootfs, runDir string, allowHostPorts []string) (*Provider, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return nil, fmt.Errorf("microvm: /dev/kvm unavailable (need KVM / nested virt): %w", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	net, err := newVMNet(allowHostPorts)
	if err != nil {
		return nil, err
	}
	return &Provider{
		fcBin: fcBin, kernel: kernel, rootfs: rootfs, runDir: runDir,
		net: net, pool: newRootfsPool(rootfs), vms: map[string]*vm{},
	}, nil
}

func (p *Provider) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	dir := filepath.Join(p.runDir, "vm-"+r.WorkspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ports.Instance{}, err
	}
	// F1.4: an instant copy-on-write clone of the golden rootfs (dm snapshot),
	// vs copying the whole image.
	rootfs, teardown, err := p.pool.clone(r.WorkspaceID, dir)
	if err != nil {
		return ports.Instance{}, fmt.Errorf("microvm: stage rootfs: %w", err)
	}
	// F1.2: give the VM a tap on the bridge + a static IP (kernel ip=).
	ip, err := p.net.allocIP()
	if err != nil {
		teardown()
		return ports.Instance{}, err
	}
	tap := tapNameForIP(ip)
	fail := func(e error) (ports.Instance, error) {
		p.net.deleteTap(tap)
		p.net.freeIP(ip)
		teardown()
		return ports.Instance{}, e
	}
	if err := p.net.createTap(tap); err != nil {
		teardown()
		p.net.freeIP(ip)
		return ports.Instance{}, err
	}
	cfg := buildConfig(VMSpec{
		KernelPath: p.kernel, RootfsPath: rootfs,
		VcpuCount: vcpusFromMillis(r.CPUMillis), MemMB: r.MemMB,
		TapDev: tap, GuestMAC: macFromIP(ip),
		BootArgs: DefaultBootArgs + " " + ipBootArg(ip, vmGateway, vmNetmask),
		Init:     vmInit, // launch hopbox-agent (F1.3)
		Env:      r.Env,  // HOPBOX_* -> kernel cmdline -> init env -> agent
	})
	_ = writeJSON(filepath.Join(dir, "config.json"), cfg) // debug aid; boot goes via the API
	logf, err := os.Create(filepath.Join(dir, "serial.log"))
	if err != nil {
		return fail(err)
	}
	// F4.1: boot over the API socket (rather than --no-api --config-file), so the
	// same VM can later be snapshotted/suspended (F4).
	sock := filepath.Join(dir, "fc.sock")
	cmd := exec.Command(p.fcBin, "--api-sock", sock)
	cmd.Stdin = nil // firecracker attaches the VM serial to stdin; don't feed it ours
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fail(fmt.Errorf("microvm: start firecracker: %w", err))
	}
	if err := waitForSocket(sock, 3*time.Second); err != nil {
		_ = cmd.Process.Kill()
		logf.Close()
		return fail(err)
	}
	if err := newFCClient(sock).boot(cfg); err != nil {
		_ = cmd.Process.Kill()
		logf.Close()
		return fail(fmt.Errorf("microvm: boot: %w", err))
	}
	v := &vm{cmd: cmd, dir: dir, ip: ip, tap: tap, sock: sock, teardown: teardown}
	p.mu.Lock()
	p.vms[r.WorkspaceID] = v
	p.mu.Unlock()
	go func() { _ = cmd.Wait(); v.done.Store(true); logf.Close() }()

	return ports.Instance{Ref: r.WorkspaceID, Phase: ports.InstanceRunning, IP: ip}, nil
}

// Close releases shared host resources (the read-only origin loop). Best-effort:
// if VMs still hold snapshots over it, the detach fails harmlessly.
func (p *Provider) Close() error {
	p.pool.close()
	return nil
}

func (p *Provider) Status(_ context.Context, ref string) (ports.Instance, error) {
	p.mu.Lock()
	v := p.vms[ref]
	suspended := v != nil && v.suspended
	p.mu.Unlock()
	switch {
	case v == nil:
		return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
	case suspended:
		return ports.Instance{Ref: ref, Phase: ports.InstanceStopped, IP: v.ip}, nil
	case v.done.Load():
		return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
	default:
		return ports.Instance{Ref: ref, Phase: ports.InstanceRunning, IP: v.ip}, nil
	}
}

// Suspend pauses the box and snapshots it to disk, then stops firecracker. The
// tap, IP, and CoW rootfs are kept so Resume can restore it.
func (p *Provider) Suspend(_ context.Context, ref string) error {
	p.mu.Lock()
	v := p.vms[ref]
	p.mu.Unlock()
	if v == nil {
		return fmt.Errorf("microvm: suspend: unknown box %s", ref)
	}
	if v.suspended {
		return nil
	}
	cl := newFCClient(v.sock)
	if err := cl.pause(); err != nil {
		return fmt.Errorf("microvm: pause: %w", err)
	}
	state, mem := v.snapPaths()
	if err := cl.snapshot(state, mem); err != nil {
		return fmt.Errorf("microvm: snapshot: %w", err)
	}
	if v.cmd.Process != nil {
		_ = v.cmd.Process.Kill()
	}
	p.mu.Lock()
	v.suspended = true
	p.mu.Unlock()
	return nil
}

// Resume restores a suspended box from its snapshot into a fresh firecracker.
func (p *Provider) Resume(_ context.Context, ref string) error {
	p.mu.Lock()
	v := p.vms[ref]
	p.mu.Unlock()
	if v == nil {
		return fmt.Errorf("microvm: resume: unknown box %s", ref)
	}
	if !v.suspended {
		return nil
	}
	// The original firecracker died holding the tap; recreate it fresh (same name,
	// which the snapshot references) so the restored VM's NIC has a live host end.
	if err := p.net.createTap(v.tap); err != nil {
		return fmt.Errorf("microvm: resume tap: %w", err)
	}
	state, mem := v.snapPaths()
	_ = os.Remove(v.sock) // the old socket is stale
	logf, err := os.OpenFile(filepath.Join(v.dir, "serial.log"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(p.fcBin, "--api-sock", v.sock)
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fmt.Errorf("microvm: resume start: %w", err)
	}
	if err := waitForSocket(v.sock, 3*time.Second); err != nil {
		_ = cmd.Process.Kill()
		logf.Close()
		return err
	}
	if err := newFCClient(v.sock).loadSnapshot(state, mem, true); err != nil {
		_ = cmd.Process.Kill()
		logf.Close()
		return fmt.Errorf("microvm: load snapshot: %w", err)
	}
	p.mu.Lock()
	v.cmd = cmd
	v.suspended = false
	v.done.Store(false)
	p.mu.Unlock()
	go func() { _ = cmd.Wait(); v.done.Store(true); logf.Close() }()
	return nil
}

func (p *Provider) Stop(_ context.Context, ref string) error { return p.kill(ref) }

func (p *Provider) Destroy(_ context.Context, ref string) error {
	_ = p.kill(ref)
	p.mu.Lock()
	v := p.vms[ref]
	delete(p.vms, ref)
	p.mu.Unlock()
	if v != nil {
		p.net.deleteTap(v.tap)
		p.net.freeIP(v.ip)
		if v.teardown != nil {
			v.teardown() // release the CoW snapshot + loop before removing the dir
		}
		return os.RemoveAll(v.dir)
	}
	return nil
}

func (p *Provider) kill(ref string) error {
	p.mu.Lock()
	v := p.vms[ref]
	p.mu.Unlock()
	if v != nil && v.cmd.Process != nil {
		_ = v.cmd.Process.Kill()
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
