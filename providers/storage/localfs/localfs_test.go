package localfs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/providers/storage/localfs"
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
