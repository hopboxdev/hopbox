package devcontainer_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestParseComposeFile(t *testing.T) {
	dir := t.TempDir()
	compose := `
services:
  postgres:
    image: postgres:16
    ports:
      - "5432:5432"
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
  redis:
    image: redis:7
    ports:
      - "6379"
    depends_on:
      - postgres
`
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}

	services, warnings := devcontainer.ParseComposeFile(path)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	pg := services["postgres"]
	if pg.Image != "postgres:16" {
		t.Errorf("postgres image = %q", pg.Image)
	}
	if len(pg.Ports) != 1 || pg.Ports[0] != "5432:5432" {
		t.Errorf("postgres ports = %v", pg.Ports)
	}
	if pg.Env["POSTGRES_PASSWORD"] != "secret" {
		t.Errorf("postgres env = %v", pg.Env)
	}

	redis := services["redis"]
	if redis.Image != "redis:7" {
		t.Errorf("redis image = %q", redis.Image)
	}
	if len(redis.DependsOn) != 1 || redis.DependsOn[0] != "postgres" {
		t.Errorf("redis depends_on = %v", redis.DependsOn)
	}

	_ = warnings // no warnings expected for this input
}

func TestParseComposeFile_Missing(t *testing.T) {
	services, warnings := devcontainer.ParseComposeFile("/nonexistent/docker-compose.yml")
	if len(services) != 0 {
		t.Errorf("expected no services, got %d", len(services))
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
}

func TestConvert_Simple(t *testing.T) {
	dir := t.TempDir()
	dc := `{
		"name": "myapp",
		"image": "mcr.microsoft.com/devcontainers/go:1.22",
		"containerEnv": {"DEBUG": "1"},
		"forwardPorts": [8080],
		"postCreateCommand": "go mod download",
		"customizations": {
			"vscode": {
				"extensions": ["golang.go"]
			}
		}
	}`
	path := filepath.Join(dir, "devcontainer.json")
	if err := os.WriteFile(path, []byte(dc), 0644); err != nil {
		t.Fatal(err)
	}

	ws, warnings, err := devcontainer.Convert(path)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if ws.Name != "myapp" {
		t.Errorf("name = %q", ws.Name)
	}

	// Image inference should add go package.
	pkgNames := make(map[string]bool)
	for _, p := range ws.Packages {
		pkgNames[p.Name] = true
	}
	if !pkgNames["go"] {
		t.Error("expected go package from image inference")
	}

	if ws.Env["DEBUG"] != "1" {
		t.Errorf("env = %v", ws.Env)
	}

	if ws.Scripts["setup"] != "go mod download" {
		t.Errorf("scripts = %v", ws.Scripts)
	}

	if ws.Editor == nil || len(ws.Editor.Extensions) != 1 || ws.Editor.Extensions[0] != "golang.go" {
		t.Errorf("editor = %+v", ws.Editor)
	}

	_ = warnings
}

func TestConvert_WithFeatures(t *testing.T) {
	dir := t.TempDir()
	dc := `{
		"name": "fullstack",
		"image": "mcr.microsoft.com/devcontainers/base:ubuntu",
		"features": {
			"ghcr.io/devcontainers/features/node:1": {"version": "20"},
			"ghcr.io/devcontainers/features/python:1": {}
		}
	}`
	path := filepath.Join(dir, "devcontainer.json")
	if err := os.WriteFile(path, []byte(dc), 0644); err != nil {
		t.Fatal(err)
	}

	ws, _, err := devcontainer.Convert(path)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	pkgs := make(map[string]string)
	for _, p := range ws.Packages {
		pkgs[p.Name] = p.Version
	}
	if _, ok := pkgs["nodejs"]; !ok {
		t.Error("expected nodejs package")
	}
	if pkgs["nodejs"] != "20" {
		t.Errorf("nodejs version = %q, want 20", pkgs["nodejs"])
	}
	if _, ok := pkgs["python3"]; !ok {
		t.Error("expected python3 package")
	}
}

func TestConvert_WithCompose(t *testing.T) {
	dir := t.TempDir()
	compose := `
services:
  db:
    image: postgres:16
    ports: ["5432:5432"]
    environment:
      POSTGRES_PASSWORD: secret
`
	dcDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcDir, "docker-compose.yml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
	dc := `{
		"name": "withdb",
		"dockerComposeFile": "docker-compose.yml",
		"containerEnv": {"DB_HOST": "localhost"}
	}`
	path := filepath.Join(dcDir, "devcontainer.json")
	if err := os.WriteFile(path, []byte(dc), 0644); err != nil {
		t.Fatal(err)
	}

	ws, _, err := devcontainer.Convert(path)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if len(ws.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(ws.Services))
	}
	db, ok := ws.Services["db"]
	if !ok {
		t.Fatal("expected db service")
	}
	if db.Image != "postgres:16" {
		t.Errorf("db image = %q", db.Image)
	}
}

func TestConvert_JSONC(t *testing.T) {
	dir := t.TempDir()
	dc := `{
		// Dev container for testing
		"name": "jsonc-test",
		"image": "ubuntu:22.04",
		"forwardPorts": [3000,], // trailing comma
	}`
	path := filepath.Join(dir, "devcontainer.json")
	if err := os.WriteFile(path, []byte(dc), 0644); err != nil {
		t.Fatal(err)
	}

	ws, _, err := devcontainer.Convert(path)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if ws.Name != "jsonc-test" {
		t.Errorf("name = %q", ws.Name)
	}
}

func TestConvert_Warnings(t *testing.T) {
	dir := t.TempDir()
	dc := `{
		"name": "warns",
		"image": "custom:latest",
		"remoteUser": "vscode",
		"mounts": ["source=data,target=/data,type=volume"],
		"build": {"dockerfile": "Dockerfile"}
	}`
	path := filepath.Join(dir, "devcontainer.json")
	if err := os.WriteFile(path, []byte(dc), 0644); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := devcontainer.Convert(path)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	// Should have warnings for: unknown image, remoteUser, mounts, build.
	if len(warnings) < 3 {
		t.Errorf("expected at least 3 warnings, got %d: %v", len(warnings), warnings)
	}
}
