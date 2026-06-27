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
	n       int                // monotonic suffix for unique dm device names
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
// plus a teardown to release it.
func (p *rootfsPool) clone(id, dir, base string) (path string, teardown func(), err error) {
	o, err := p.originFor(base)
	if err != nil { // fallback: full copy
		log.Printf("microvm: CoW unavailable for %s (%v); copying", base, err)
		f := filepath.Join(dir, "rootfs.ext4")
		if cerr := copyFile(base, f); cerr != nil {
			return "", nil, cerr
		}
		return f, func() {}, nil
	}
	cow := filepath.Join(dir, "cow.img")
	if err := truncateFile(cow, cowHeadroom); err != nil {
		return "", nil, err
	}
	cowLoop, err := losetup(cow, false)
	if err != nil {
		return "", nil, err
	}
	p.mu.Lock()
	p.n++
	name := fmt.Sprintf("hopbox-%s-%d", dmSafe(id), p.n)
	p.mu.Unlock()
	// snapshot: origin (ro, shared) + cow store, persistent, 8-sector chunks.
	table := fmt.Sprintf("0 %d snapshot %s %s P 8", o.sectors, o.loop, cowLoop)
	if err := run("dmsetup", "create", name, "--table", table); err != nil {
		_ = losetupDetach(cowLoop)
		return "", nil, fmt.Errorf("microvm: dm snapshot: %w", err)
	}
	teardown = func() {
		// --retry: firecracker may still be releasing the device right after kill.
		_ = run("dmsetup", "remove", "--retry", name)
		_ = losetupDetach(cowLoop)
	}
	return "/dev/mapper/" + name, teardown, nil
}

func (p *rootfsPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, o := range p.origins {
		_ = losetupDetach(o.loop)
	}
}

// --- host helpers ---

// originLoop returns a read-only loop over base, reusing one that already exists
// (so a boxd restart doesn't accumulate a loop per start) or creating a new one.
func originLoop(base string) (string, error) {
	if out, err := exec.Command("losetup", "-j", base).Output(); err == nil {
		line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		if loop := strings.SplitN(line, ":", 2)[0]; strings.HasPrefix(loop, "/dev/loop") {
			return loop, nil
		}
	}
	return losetup(base, true)
}

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
