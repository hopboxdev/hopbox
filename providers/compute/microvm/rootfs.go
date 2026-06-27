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

// rootfsPool hands each VM a copy-on-write clone of the golden rootfs via a
// device-mapper snapshot over a single shared read-only origin loop — instant,
// versus copying the whole image. If loop/dm setup is unavailable it falls back
// to a full file copy, so the provider still works (just slower).
type rootfsPool struct {
	base       string
	originLoop string // read-only loop over base (shared origin); "" => CoW unavailable
	sectors    int64  // origin size in 512-byte sectors
	mu         sync.Mutex
	n          int // monotonic suffix for unique dm device names
}

func newRootfsPool(base string) *rootfsPool {
	p := &rootfsPool{base: base}
	loop, err := originLoop(base)
	if err != nil {
		log.Printf("microvm: CoW unavailable (%v); falling back to full rootfs copies", err)
		return p
	}
	sz, err := blockSectors(loop)
	if err != nil {
		_ = losetupDetach(loop)
		log.Printf("microvm: CoW unavailable (%v); falling back to full rootfs copies", err)
		return p
	}
	p.originLoop, p.sectors = loop, sz
	return p
}

// clone returns a per-VM rootfs path (a dm snapshot device, or a copied file)
// plus a teardown to release it.
func (p *rootfsPool) clone(id, dir string) (path string, teardown func(), err error) {
	if p.originLoop == "" { // fallback: full copy
		f := filepath.Join(dir, "rootfs.ext4")
		if err := copyFile(p.base, f); err != nil {
			return "", nil, err
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
	table := fmt.Sprintf("0 %d snapshot %s %s P 8", p.sectors, p.originLoop, cowLoop)
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
	if p.originLoop != "" {
		_ = losetupDetach(p.originLoop)
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
