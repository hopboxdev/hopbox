package packages_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/packages"
)

func TestStateLoad_Missing(t *testing.T) {
	pkgs, err := packages.LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadState on missing file: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected empty slice, got %d packages", len(pkgs))
	}
}

func TestStateSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := []packages.Package{
		{Name: "htop", Backend: packages.Apt},
		{Name: "ripgrep", Backend: packages.Static, URL: "https://example.com/rg.tar.gz"},
		{Name: "nodejs", Backend: packages.Nix, Version: "20"},
	}
	if err := packages.SaveState(path, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := packages.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Backend != want[i].Backend || got[i].Version != want[i].Version {
			t.Errorf("package %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStateSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write initial state.
	if err := packages.SaveState(path, []packages.Package{{Name: "a"}}); err != nil {
		t.Fatal(err)
	}
	// Overwrite — should not corrupt if the process is interrupted.
	if err := packages.SaveState(path, []packages.Package{{Name: "b"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := packages.LoadState(path)
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("expected [b], got %+v", got)
	}
	// No leftover .tmp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected file: %s", e.Name())
		}
	}
}
