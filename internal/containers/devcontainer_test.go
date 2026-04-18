package containers

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDevcontainer_IsValidJSON(t *testing.T) {
	raw := DefaultDevcontainer()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("default devcontainer is not valid JSON: %v", err)
	}
	if _, ok := obj["image"]; !ok {
		t.Errorf("default devcontainer missing 'image' key")
	}
	if _, ok := obj["features"]; !ok {
		t.Errorf("default devcontainer missing 'features' key")
	}
}

func TestCanonicalHash_Deterministic(t *testing.T) {
	a := []byte(`{"name":"x","features":{"a":{},"b":{}}}`)
	b := []byte(`{"features":{"b":{},"a":{}},"name":"x"}`)
	ha, err := CanonicalHash(a)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := CanonicalHash(b)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha != hb {
		t.Errorf("canonical forms should hash identically: %q vs %q", ha, hb)
	}
}

func TestCanonicalHash_Differs(t *testing.T) {
	a := []byte(`{"name":"x"}`)
	b := []byte(`{"name":"y"}`)
	ha, _ := CanonicalHash(a)
	hb, _ := CanonicalHash(b)
	if ha == hb {
		t.Errorf("different content should hash differently")
	}
}

func TestCanonicalHash_Length(t *testing.T) {
	h, err := CanonicalHash([]byte(`{}`))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if len(h) != 12 {
		t.Errorf("hash len: got %d, want 12", len(h))
	}
}

func TestCanonicalHash_InvalidJSON(t *testing.T) {
	if _, err := CanonicalHash([]byte(`{not json}`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadDevcontainer_Missing(t *testing.T) {
	_, err := ReadDevcontainer(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestReadDevcontainer_Present(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devcontainer.json")
	want := []byte(`{"name":"t"}`)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDevcontainer(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content mismatch: got %s, want %s", got, want)
	}
}
