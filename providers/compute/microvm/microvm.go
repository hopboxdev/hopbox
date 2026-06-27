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
	cmd      *exec.Cmd
	dir      string
	ip       string      // allocated guest IP
	tap      string      // host tap device
	teardown func()      // release the CoW rootfs clone
	done     atomic.Bool // set when the firecracker process exits
}

var _ ports.Compute = (*Provider)(nil)

// New builds the provider. It requires /dev/kvm and sets up the VM bridge.
func New(fcBin, kernel, rootfs, runDir string) (*Provider, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return nil, fmt.Errorf("microvm: /dev/kvm unavailable (need KVM / nested virt): %w", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	net, err := newVMNet()
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
	if err := writeJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		return fail(err)
	}
	logf, err := os.Create(filepath.Join(dir, "serial.log"))
	if err != nil {
		return fail(err)
	}
	cmd := exec.Command(p.fcBin, "--no-api", "--config-file", filepath.Join(dir, "config.json"))
	cmd.Stdin = nil // firecracker attaches the VM serial to stdin; don't feed it ours
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fail(fmt.Errorf("microvm: start firecracker: %w", err))
	}
	v := &vm{cmd: cmd, dir: dir, ip: ip, tap: tap, teardown: teardown}
	p.mu.Lock()
	p.vms[r.WorkspaceID] = v
	p.mu.Unlock()
	go func() { _ = cmd.Wait(); v.done.Store(true); logf.Close() }()

	return ports.Instance{Ref: r.WorkspaceID, Phase: ports.InstanceRunning, IP: ip}, nil
}

func (p *Provider) Status(_ context.Context, ref string) (ports.Instance, error) {
	p.mu.Lock()
	v := p.vms[ref]
	p.mu.Unlock()
	if v == nil || v.done.Load() {
		return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
	}
	return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
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
