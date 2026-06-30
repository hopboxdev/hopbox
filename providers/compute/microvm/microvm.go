//go:build firecracker

package microvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	fcBin     string      // firecracker binary
	kernel    string      // vmlinux
	imagesDir string      // catalog of base images: <name>.ext4
	runDir    string      // per-VM working dirs live here
	net       *vmNet      // host bridge + tap + IP allocation
	pool      *rootfsPool // CoW rootfs clones (dm snapshot, or copy fallback)

	mu  sync.Mutex
	vms map[string]*vm
}

type vm struct {
	cmd       *exec.Cmd
	dir       string
	image     string      // base image name (to rebuild the rootfs on reattach)
	ip        string      // allocated guest IP
	tap       string      // host tap device
	sock      string      // firecracker API socket (for snapshot/suspend, F4)
	teardown  func()      // release the CoW rootfs clone
	suspended bool        // snapshotted to disk; firecracker not running
	done      atomic.Bool // set when the firecracker process exits
}

// vmMeta is the on-disk record (vm.json in the box dir) that lets a restarted
// boxd reattach the box: its disk (cow.img) + snapshot are already on disk.
type vmMeta struct {
	Image     string `json:"image"`
	IP        string `json:"ip"`
	Tap       string `json:"tap"`
	Suspended bool   `json:"suspended"`
}

func (v *vm) persist() {
	_ = writeJSON(filepath.Join(v.dir, "vm.json"), vmMeta{
		Image: v.image, IP: v.ip, Tap: v.tap, Suspended: v.suspended,
	})
}

var (
	_ ports.Compute   = (*Provider)(nil)
	_ ports.Suspender = (*Provider)(nil)
)

func (v *vm) snapPaths() (state, mem string) {
	return filepath.Join(v.dir, "snapshot"), filepath.Join(v.dir, "mem")
}

// New builds the provider. It requires /dev/kvm and sets up the VM bridge +
// egress fence. imagesDir is the base-image catalog (a box's image name maps to
// <imagesDir>/<name>.ext4). allowHostPorts are the host ports a box may reach.
func New(fcBin, kernel, imagesDir, runDir string, allowHostPorts []string, netCfg NetConfig) (*Provider, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return nil, fmt.Errorf("microvm: /dev/kvm unavailable (need KVM / nested virt): %w", err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	net, err := newVMNet(allowHostPorts, netCfg)
	if err != nil {
		return nil, err
	}
	p := &Provider{
		fcBin: fcBin, kernel: kernel, imagesDir: imagesDir, runDir: runDir,
		net: net, pool: newRootfsPool(), vms: map[string]*vm{},
	}
	p.reattach() // restore boxes from a previous run (restart / host reboot)
	return p, nil
}

// reattach restores boxes from a previous run so a boxd restart (or host reboot)
// doesn't lose them — their disk (cow.img) and FC snapshot are on disk. Each
// box that was cleanly suspended comes back as suspended; the reconciler resumes
// it on the next connect. Boxes without a snapshot (a non-graceful exit) are left
// for the reconciler to re-provision (Provision reuses cow.img, so disk survives).
func (p *Provider) reattach() {
	entries, err := os.ReadDir(p.runDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "vm-") {
			continue
		}
		ref := strings.TrimPrefix(e.Name(), "vm-")
		dir := filepath.Join(p.runDir, e.Name())
		var m vmMeta
		if err := readJSON(filepath.Join(dir, "vm.json"), &m); err != nil {
			continue
		}
		if state, _ := (&vm{dir: dir}).snapPaths(); !fileExists(state) {
			continue // no snapshot: let the reconciler re-provision (cow.img persists)
		}
		base, err := p.imagePath(m.Image)
		if err != nil {
			log.Printf("microvm: reattach %s: %v; skipping", ref, err)
			continue
		}
		_, teardown, err := p.pool.clone(ref, dir, base) // rebuild the CoW rootfs device
		if err != nil {
			log.Printf("microvm: reattach %s rootfs: %v; skipping", ref, err)
			continue
		}
		p.net.reserveIP(m.IP)
		p.mu.Lock()
		p.vms[ref] = &vm{
			dir: dir, image: m.Image, ip: m.IP, tap: m.Tap,
			sock: filepath.Join(dir, "fc.sock"), teardown: teardown, suspended: true,
		}
		p.mu.Unlock()
		log.Printf("microvm: reattached box %s (image %s, ip %s) — suspended; resumes on connect", ref, m.Image, m.IP)
	}
}

// imagePath resolves a box's image name to its base rootfs in the catalog.
func (p *Provider) imagePath(image string) (string, error) {
	base := filepath.Join(p.imagesDir, image+".ext4")
	if _, err := os.Stat(base); err != nil {
		return "", fmt.Errorf("microvm: unknown image %q (no %s)", image, base)
	}
	return base, nil
}

// defaultDNS is the resolver set in guests when the request doesn't specify one.
const defaultDNS = "1.1.1.1 8.8.8.8"

func (p *Provider) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	dir := filepath.Join(p.runDir, "vm-"+r.WorkspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ports.Instance{}, err
	}
	// Resolve the box's image in the catalog, then make an instant copy-on-write
	// clone of it (dm snapshot) vs copying the whole image.
	base, err := p.imagePath(r.ImageRef)
	if err != nil {
		return ports.Instance{}, err
	}
	rootfs, teardown, err := p.pool.clone(r.WorkspaceID, dir, base)
	if err != nil {
		return ports.Instance{}, fmt.Errorf("microvm: stage rootfs: %w", err)
	}
	// F1.2: give the VM a tap on the bridge + a static IP (kernel ip=).
	ip, err := p.net.allocIP()
	if err != nil {
		teardown()
		return ports.Instance{}, err
	}
	tap := p.net.cfg.tapName(ip)
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
	// A block-device mount (the dev-env's persistent home) is attached as a second
	// drive (/dev/vdb); the agent mounts it at the mount target. Copy the env so we
	// don't mutate the caller's map.
	env := make(map[string]string, len(r.Env)+3)
	for k, v := range r.Env {
		env[k] = v
	}
	// A microVM guest gets its IP+gateway from the kernel ip= arg but no DNS, and
	// the catalog images point resolv.conf at a dead stub — so the agent writes a
	// working resolv.conf from HOPBOX_DNS. Overridable via the request env.
	if _, ok := env["HOPBOX_DNS"]; !ok {
		env["HOPBOX_DNS"] = defaultDNS
	}
	homeDrive := ""
	for _, m := range r.Mounts {
		if m.Device {
			homeDrive = m.Source
			env["HOPBOX_HOME_DEV"], env["HOPBOX_HOME_MOUNT"] = "/dev/vdb", m.Target
			break
		}
	}
	cfg := buildConfig(VMSpec{
		KernelPath: p.kernel, RootfsPath: rootfs,
		VcpuCount: vcpusFromMillis(r.CPUMillis), MemMB: r.MemMB,
		TapDev: tap, GuestMAC: macFromIP(ip),
		BootArgs:  DefaultBootArgs + " " + ipBootArg(ip, p.net.cfg.gateway(), p.net.cfg.netmask()),
		Init:      vmInit, // launch hopbox-agent (F1.3)
		Env:       env,    // HOPBOX_* -> kernel cmdline -> init env -> agent
		HomeDrive: homeDrive,
	})
	_ = writeJSON(filepath.Join(dir, "config.json"), cfg) // debug aid; boot goes via the API
	logf, err := os.Create(filepath.Join(dir, "serial.log"))
	if err != nil {
		return fail(err)
	}
	// F4.1: boot over the API socket (rather than --no-api --config-file), so the
	// same VM can later be snapshotted/suspended (F4).
	sock := filepath.Join(dir, "fc.sock")
	_ = os.Remove(sock) // a re-provision reuses the dir; a stale socket would fool waitForSocket
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
	v := &vm{cmd: cmd, dir: dir, image: r.ImageRef, ip: ip, tap: tap, sock: sock, teardown: teardown}
	v.persist() // record enough to reattach this box after a restart
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
	v.persist()
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
	v.persist()
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

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

// Images lists the catalog: <name> for each <imagesDir>/<name>.ext4.
func (p *Provider) Images() []string {
	entries, err := os.ReadDir(p.imagesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".ext4"); ok && !e.IsDir() {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}
