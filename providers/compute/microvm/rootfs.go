//go:build firecracker

package microvm

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// cowHeadroom is the per-VM copy-on-write store size (sparse) — space for all
// writes during the box's life. Sparse, so it costs only what's actually written.
const cowHeadroom = 8 << 30 // 8 GiB

// rootfsPool hands each VM a copy-on-write clone of a base image via a
// device-mapper snapshot over a shared read-only origin loop — instant, versus
// copying the whole image. It keeps one origin per base image (the catalog), so
// multiple images coexist. If loop/dm is unavailable it falls back to a copy.
type rootfsPool struct {
	mu      sync.Mutex
	origins map[string]*origin // base image path -> shared read-only origin
}

type origin struct {
	loop    string // read-only loop over the base image
	sectors int64  // size in 512-byte sectors
}

func newRootfsPool() *rootfsPool { return &rootfsPool{origins: map[string]*origin{}} }

// originFor lazily creates (or reuses) the read-only origin loop for a base image.
func (p *rootfsPool) originFor(base string) (*origin, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o, ok := p.origins[base]; ok {
		return o, nil
	}
	loop, err := originLoop(base)
	if err != nil {
		return nil, err
	}
	sz, err := blockSectors(loop)
	if err != nil {
		_ = losetupDetach(loop)
		return nil, err
	}
	o := &origin{loop: loop, sectors: sz}
	p.origins[base] = o
	return o, nil
}

// clone returns a per-VM rootfs path (a dm snapshot of base, or a copied file)
// plus a teardown to release it. It is idempotent and durable: an existing
// cow.img is REUSED (the box's disk writes persist across restart/reboot), and
// the dm device is rebuilt only if the kernel lost it (host reboot) — so calling
// clone again on startup reattaches the same disk.
func (p *rootfsPool) clone(id, dir, base string) (path string, teardown func(), err error) {
	o, err := p.originFor(base)
	if err != nil { // fallback: full copy (no CoW)
		log.Printf("microvm: CoW unavailable for %s (%v); copying", base, err)
		f := filepath.Join(dir, "rootfs.ext4")
		if _, serr := os.Stat(f); serr != nil { // reuse an existing copy if present
			if cerr := copyFile(base, f); cerr != nil {
				return "", nil, cerr
			}
		}
		return f, func() {}, nil
	}
	cow := filepath.Join(dir, "cow.img")
	if _, serr := os.Stat(cow); serr != nil { // fresh box: sparse cow store
		if err := truncateFile(cow, cowHeadroom); err != nil {
			return "", nil, err
		}
	} // else: reuse the existing cow.img — the box's disk writes persist

	// Deterministic name (one snapshot per box), so a restart can recompute it.
	name := "hopbox-" + dmSafe(id)
	dev := "/dev/mapper/" + name
	td := p.teardownFor(name, cow)
	if _, serr := os.Stat(dev); serr == nil {
		return dev, td, nil // dm device survived (boxd restart) — reuse it
	}
	cowLoop, err := loopFor(cow, false) // reuse a loop over cow, else create one
	if err != nil {
		return "", nil, err
	}
	// snapshot: origin (ro, shared) + cow store, persistent, 8-sector chunks.
	table := fmt.Sprintf("0 %d snapshot %s %s P 8", o.sectors, o.loop, cowLoop)
	if err := run("dmsetup", "create", name, "--table", table); err != nil {
		_ = losetupDetach(cowLoop)
		return "", nil, fmt.Errorf("microvm: dm snapshot: %w", err)
	}
	return dev, td, nil
}

// teardownFor releases a box's dm snapshot + the loop over its cow store.
func (p *rootfsPool) teardownFor(name, cow string) func() {
	return func() {
		// --retry: firecracker may still be releasing the device right after kill.
		_ = run("dmsetup", "remove", "--retry", name)
		if loop := findLoop(cow); loop != "" {
			_ = losetupDetach(loop)
		}
	}
}

func (p *rootfsPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, o := range p.origins {
		_ = losetupDetach(o.loop)
	}
}

// --- host helpers ---

// findLoop returns an existing loop device backed by file, or "" if none.
func findLoop(file string) string {
	if out, err := exec.Command("losetup", "-j", file).Output(); err == nil {
		line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		if loop := strings.SplitN(line, ":", 2)[0]; strings.HasPrefix(loop, "/dev/loop") {
			return loop
		}
	}
	return ""
}

// loopFor reuses an existing loop over file (so a restart doesn't accumulate
// loops) or creates a new one.
func loopFor(file string, readonly bool) (string, error) {
	if l := findLoop(file); l != "" {
		return l, nil
	}
	return losetup(file, readonly)
}

// originLoop returns a read-only loop over a base image (shared origin).
func originLoop(base string) (string, error) { return loopFor(base, true) }

func losetup(file string, readonly bool) (string, error) {
	args := []string{"--find", "--show"}
	if readonly {
		args = append(args, "--read-only")
	}
	args = append(args, file)
	out, err := exec.Command("losetup", args...).Output()
	if err != nil {
		return "", fmt.Errorf("losetup %s: %w", file, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func losetupDetach(loop string) error { return run("losetup", "-d", loop) }

func blockSectors(dev string) (int64, error) {
	out, err := exec.Command("blockdev", "--getsz", dev).Output()
	if err != nil {
		return 0, fmt.Errorf("blockdev --getsz %s: %w", dev, err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

func truncateFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// dmSafe makes an id usable in a device-mapper name ([a-zA-Z0-9_-], bounded).
func dmSafe(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
