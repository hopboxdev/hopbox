package containers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashTemplates(t *testing.T) {
	dir := t.TempDir()

	// Create fake template files
	if err := os.MkdirAll(filepath.Join(dir, "stacks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.base"), []byte("FROM ubuntu:24.04"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stacks", "tools.sh"), []byte("apt install stuff"), 0644); err != nil {
		t.Fatal(err)
	}

	hash1, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 == "" {
		t.Fatal("expected non-empty hash")
	}

	// Same content = same hash
	hash2, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("expected stable hash, got %s and %s", hash1, hash2)
	}

	// Change content = different hash
	if err := os.WriteFile(filepath.Join(dir, "stacks", "tools.sh"), []byte("apt install different"), 0644); err != nil {
		t.Fatal(err)
	}
	hash3, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 == hash3 {
		t.Error("expected different hash after content change")
	}
}
