package hostd

import (
	"path/filepath"
	"testing"
)

func TestPortAllocator_Allocate(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	port, err := pa.Allocate("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if port < 51820 || port > 51830 {
		t.Fatalf("port %d out of range", port)
	}
}

func TestPortAllocator_Release(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	port, _ := pa.Allocate("ws-1")
	if err := pa.Release("ws-1"); err != nil {
		t.Fatal(err)
	}

	// Same port should be available again
	port2, _ := pa.Allocate("ws-2")
	if port2 != port {
		t.Fatalf("expected reuse of port %d, got %d", port, port2)
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	dir := t.TempDir()
	pa, err := NewPortAllocator(51820, 51822, filepath.Join(dir, "ports.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pa.Allocate("ws-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := pa.Allocate("ws-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := pa.Allocate("ws-3"); err != nil {
		t.Fatal(err)
	}

	_, err = pa.Allocate("ws-4")
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestPortAllocator_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ports.json")

	pa1, _ := NewPortAllocator(51820, 51830, path)
	if _, err := pa1.Allocate("ws-1"); err != nil {
		t.Fatal(err)
	}
	port1, _ := pa1.Get("ws-1")

	// Reload from disk
	pa2, _ := NewPortAllocator(51820, 51830, path)
	port2, err := pa2.Get("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if port1 != port2 {
		t.Fatalf("port not persisted: got %d, want %d", port2, port1)
	}
}

func TestPortAllocator_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	pa, _ := NewPortAllocator(51820, 51830, filepath.Join(dir, "ports.json"))

	if _, err := pa.Allocate("ws-1"); err != nil {
		t.Fatal(err)
	}
	_, err := pa.Allocate("ws-1")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}
