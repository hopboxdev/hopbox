package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "hop")
	if err := os.WriteFile(binPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := atomicReplace(binPath, []byte("new")); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("expected 'new', got %q", string(data))
	}

	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("expected 0755, got %o", info.Mode().Perm())
	}
}

func TestAtomicReplace_CleansUpOnFailure(t *testing.T) {
	// Non-existent directory → Rename will fail.
	err := atomicReplace("/no/such/dir/hop", []byte("data"))
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
	// The .new file should not be left behind.
	if _, statErr := os.Stat("/no/such/dir/hop.new"); statErr == nil {
		t.Fatal(".new file should have been cleaned up")
	}
}

func TestResolveLocalPaths(t *testing.T) {
	paths := resolveLocalPaths("/some/dist")
	if paths.client != "/some/dist/hop" {
		t.Errorf("client = %q, want /some/dist/hop", paths.client)
	}
	if paths.helper != "/some/dist/hop-helper" {
		t.Errorf("helper = %q, want /some/dist/hop-helper", paths.helper)
	}
	if paths.agent != "/some/dist/hop-agent-linux" {
		t.Errorf("agent = %q, want /some/dist/hop-agent-linux", paths.agent)
	}
}
