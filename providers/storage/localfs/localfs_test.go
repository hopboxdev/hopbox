package localfs_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

func TestEnsureHomeCreatesDir(t *testing.T) {
	root := t.TempDir()
	p := localfs.New(root)
	m, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if m.Source != filepath.Join(root, "w1") {
		t.Fatalf("source=%s", m.Source)
	}
	if m.Target != "/home/dev" {
		t.Fatalf("target=%s", m.Target)
	}
	if fi, err := os.Stat(m.Source); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: err=%v", err)
	}
}

func TestEnsureHomeIdempotent(t *testing.T) {
	root := t.TempDir()
	p := localfs.New(root)
	ctx := context.Background()
	_, _ = p.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"})
	if _, err := p.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"}); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
}

func TestDeleteRemovesDir(t *testing.T) {
	root := t.TempDir()
	p := localfs.New(root)
	ctx := context.Background()
	m, _ := p.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"})
	if err := p.Delete(ctx, m.Source); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(m.Source); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
}

func TestDeleteRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	p := localfs.New(root)
	if err := p.Delete(context.Background(), "/etc"); err == nil {
		t.Fatal("expected Delete to refuse a path outside root")
	}
	// the root itself must also be refused
	if err := p.Delete(context.Background(), root); err == nil {
		t.Fatal("expected Delete to refuse the root dir itself")
	}
}

func TestEnsureHomeRejectsBadID(t *testing.T) {
	root := t.TempDir()
	p := localfs.New(root)
	for _, bad := range []string{"", "../escape", "a/b", ".."} {
		if _, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: bad}); err == nil {
			t.Errorf("EnsureHome(%q) should have errored", bad)
		}
	}
}

// Block mode creates a per-workspace ext4 image and reports it as a Device mount
// (the microVM home). Skipped where mkfs.ext4 isn't available (e.g. macOS).
func TestEnsureHomeBlock(t *testing.T) {
	if _, err := exec.LookPath("mkfs.ext4"); err != nil {
		t.Skip("no mkfs.ext4")
	}
	root := t.TempDir()
	p := localfs.NewBlock(root, 64)
	m, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !m.Device || m.Source != filepath.Join(root, "w1.ext4") || m.Target != "/home/dev" {
		t.Fatalf("block mount wrong: %+v", m)
	}
	if fi, err := os.Stat(m.Source); err != nil || fi.Size() != 64<<20 {
		t.Fatalf("image stat: %v size=%d", err, fi.Size())
	}
	if m2, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"}); err != nil || m2.Source != m.Source {
		t.Fatalf("not idempotent: %+v %v", m2, err)
	}
	if err := p.Delete(context.Background(), m.Source); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(m.Source); !os.IsNotExist(err) {
		t.Fatalf("image not removed: %v", err)
	}
}
