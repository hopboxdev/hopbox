package devcontainer_test

import (
	"encoding/json"
	"testing"

	"github.com/hopboxdev/hopbox/internal/devcontainer"
)

func TestStripJSONC_Comments(t *testing.T) {
	input := `{
		// this is a comment
		"name": "test", // inline
		/* block
		   comment */
		"image": "ubuntu"
	}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	// Should parse as valid JSON.
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("not valid JSON after strip: %v", err)
	}
	if m["name"] != "test" || m["image"] != "ubuntu" {
		t.Errorf("got %v", m)
	}
}

func TestStripJSONC_TrailingCommas(t *testing.T) {
	input := `{"items": ["a", "b",], "key": "val",}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, got)
	}
}

func TestStripJSONC_StringsPreserved(t *testing.T) {
	input := `{"url": "https://example.com/path // not a comment"}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if m["url"] != "https://example.com/path // not a comment" {
		t.Errorf("string mangled: %v", m["url"])
	}
}

func TestFeatureToPackages(t *testing.T) {
	features := map[string]json.RawMessage{
		"ghcr.io/devcontainers/features/node:1":         json.RawMessage(`{"version": "20"}`),
		"ghcr.io/devcontainers/features/go:1":           json.RawMessage(`{}`),
		"ghcr.io/devcontainers/features/unknown-tool:1": json.RawMessage(`{}`),
	}
	pkgs, warnings := devcontainer.FeatureToPackages(features)

	// Should have node and go mapped.
	names := make(map[string]bool)
	for _, p := range pkgs {
		names[p.Name] = true
	}
	if !names["nodejs"] {
		t.Error("expected nodejs package")
	}
	if !names["go"] {
		t.Error("expected go package")
	}

	// Should warn about unknown feature.
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
}

func TestFeatureToPackages_NodeVersion(t *testing.T) {
	features := map[string]json.RawMessage{
		"ghcr.io/devcontainers/features/node:1": json.RawMessage(`{"version": "20"}`),
	}
	pkgs, _ := devcontainer.FeatureToPackages(features)
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Version != "20" {
		t.Errorf("version = %q, want %q", pkgs[0].Version, "20")
	}
}

func TestImageToPackages(t *testing.T) {
	tests := []struct {
		image   string
		wantPkg string
		wantVer string
	}{
		{"mcr.microsoft.com/devcontainers/go:1.22", "go", ""},
		{"mcr.microsoft.com/devcontainers/python:3.12", "python3", ""},
		{"mcr.microsoft.com/devcontainers/typescript-node:20", "nodejs", "20"},
		{"mcr.microsoft.com/devcontainers/base:ubuntu", "", ""},
		{"custom-image:latest", "", ""},
	}
	for _, tt := range tests {
		pkgs, _ := devcontainer.ImageToPackages(tt.image)
		if tt.wantPkg == "" {
			if len(pkgs) != 0 {
				t.Errorf("image %q: expected no packages, got %v", tt.image, pkgs)
			}
			continue
		}
		if len(pkgs) != 1 || pkgs[0].Name != tt.wantPkg {
			t.Errorf("image %q: got %v, want %s", tt.image, pkgs, tt.wantPkg)
		}
		if tt.wantVer != "" && pkgs[0].Version != tt.wantVer {
			t.Errorf("image %q: version = %q, want %q", tt.image, pkgs[0].Version, tt.wantVer)
		}
	}
}
