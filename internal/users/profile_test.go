package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()
	if p.Multiplexer.Tool != "zellij" {
		t.Errorf("multiplexer: got %q, want %q", p.Multiplexer.Tool, "zellij")
	}
	if p.Editor.Tool != "neovim" {
		t.Errorf("editor: got %q, want %q", p.Editor.Tool, "neovim")
	}
	if p.Shell.Tool != "bash" {
		t.Errorf("shell: got %q, want %q", p.Shell.Tool, "bash")
	}
	if p.Runtimes.Node != "none" {
		t.Errorf("node: got %q, want %q", p.Runtimes.Node, "none")
	}
	if p.Runtimes.Python != "none" {
		t.Errorf("python: got %q, want %q", p.Runtimes.Python, "none")
	}
	if p.Runtimes.Go != "none" {
		t.Errorf("go: got %q, want %q", p.Runtimes.Go, "none")
	}
	if p.Runtimes.Rust != "none" {
		t.Errorf("rust: got %q, want %q", p.Runtimes.Rust, "none")
	}
	if len(p.Tools.Extras) != 5 {
		t.Errorf("extras: got %d tools, want 5", len(p.Tools.Extras))
	}
}

func TestProfileSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.toml")

	p := DefaultProfile()
	p.Editor.Tool = "vim"
	p.Runtimes.Go = "latest"

	if err := SaveProfile(path, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Editor.Tool != "vim" {
		t.Errorf("editor: got %q, want %q", loaded.Editor.Tool, "vim")
	}
	if loaded.Runtimes.Go != "latest" {
		t.Errorf("go: got %q, want %q", loaded.Runtimes.Go, "latest")
	}
}

func TestProfileHash(t *testing.T) {
	p1 := DefaultProfile()
	p2 := DefaultProfile()
	p3 := DefaultProfile()
	p3.Runtimes.Go = "latest"

	h1 := p1.Hash()
	h2 := p2.Hash()
	h3 := p3.Hash()

	if h1 != h2 {
		t.Errorf("same profiles should have same hash: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Error("different profiles should have different hashes")
	}
	if len(h1) != 12 {
		t.Errorf("hash should be 12 chars, got %d", len(h1))
	}
}

func TestResolveProfile(t *testing.T) {
	dir := t.TempDir()

	// No profile anywhere → nil
	p, err := ResolveProfile(dir, "default")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p != nil {
		t.Error("expected nil when no profile exists")
	}

	// Save user-level profile
	userProfile := DefaultProfile()
	userProfile.Editor.Tool = "vim"
	if err := SaveProfile(filepath.Join(dir, "profile.toml"), userProfile); err != nil {
		t.Fatalf("save user profile: %v", err)
	}

	// Resolve without box profile → user profile
	p, err = ResolveProfile(dir, "default")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p == nil {
		t.Fatal("expected user profile")
	}
	if p.Editor.Tool != "vim" {
		t.Errorf("editor: got %q, want %q", p.Editor.Tool, "vim")
	}

	// Save box-level profile → overrides user
	boxDir := filepath.Join(dir, "boxes", "mybox")
	if err := os.MkdirAll(boxDir, 0755); err != nil {
		t.Fatal(err)
	}
	boxProfile := DefaultProfile()
	boxProfile.Editor.Tool = "none"
	if err := SaveProfile(filepath.Join(boxDir, "profile.toml"), boxProfile); err != nil {
		t.Fatalf("save box profile: %v", err)
	}

	p, err = ResolveProfile(dir, "mybox")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Editor.Tool != "none" {
		t.Errorf("editor: got %q, want %q (box should override user)", p.Editor.Tool, "none")
	}
}
