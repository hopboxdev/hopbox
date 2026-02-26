# Devcontainer Import Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** `hop init --from devcontainer.json` reads a devcontainer.json and generates a `hopbox.yaml` with best-effort field mapping.

**Architecture:** A new `internal/devcontainer` package with a `Convert` function that parses devcontainer.json (with JSONC support), maps fields to a `manifest.Workspace`, and returns warnings for unmapped fields. The `hop init` command gets a `--from` flag to call this converter. Docker Compose files referenced by `dockerComposeFile` are parsed and mapped to hopbox services.

**Tech Stack:** Go standard library (`encoding/json`, `os`, `regexp`, `strings`). `gopkg.in/yaml.v3` for compose parsing. No new dependencies.

---

### Task 1: JSONC parser and devcontainer struct

**Files:**
- Create: `internal/devcontainer/devcontainer.go`
- Test: `internal/devcontainer/devcontainer_test.go`

**Step 1: Write the failing tests**

```go
// internal/devcontainer/devcontainer_test.go
package devcontainer_test

import (
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
```

Note: add `"encoding/json"` to imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run 'TestStripJSONC' -v`
Expected: compilation error — `devcontainer.StripJSONC` undefined

**Step 3: Write minimal implementation**

```go
// internal/devcontainer/devcontainer.go
package devcontainer

import (
	"encoding/json"
	"regexp"
	"strings"
)

// devcontainerJSON represents the relevant fields of a devcontainer.json file.
type devcontainerJSON struct {
	Name               string                       `json:"name"`
	Image              string                       `json:"image"`
	Features           map[string]json.RawMessage    `json:"features"`
	ForwardPorts       []int                         `json:"forwardPorts"`
	ContainerEnv       map[string]string             `json:"containerEnv"`
	PostCreateCommand  stringOrSlice                 `json:"postCreateCommand"`
	PostStartCommand   stringOrSlice                 `json:"postStartCommand"`
	Customizations     map[string]json.RawMessage    `json:"customizations"`
	Mounts             []any                         `json:"mounts"`
	DockerComposeFile  stringOrSlice                 `json:"dockerComposeFile"`
	RemoteUser         string                        `json:"remoteUser"`
	Build              json.RawMessage               `json:"build"`
	RunArgs            []string                      `json:"runArgs"`
}

// stringOrSlice handles devcontainer fields that can be either a string or []string.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = []string{str}
		return nil
	}
	var slice []string
	if err := json.Unmarshal(data, &slice); err != nil {
		return err
	}
	*s = slice
	return nil
}

func (s stringOrSlice) String() string {
	return strings.Join(s, " && ")
}

// StripJSONC removes // comments, /* */ comments, and trailing commas from JSONC.
// Preserves strings containing comment-like sequences.
func StripJSONC(data []byte) ([]byte, error) {
	s := string(data)
	var result strings.Builder
	i := 0
	for i < len(s) {
		// String literal — copy verbatim.
		if s[i] == '"' {
			result.WriteByte(s[i])
			i++
			for i < len(s) {
				result.WriteByte(s[i])
				if s[i] == '\\' && i+1 < len(s) {
					i++
					result.WriteByte(s[i])
				} else if s[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		// Line comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}

	// Remove trailing commas before } or ].
	re := regexp.MustCompile(`,\s*([}\]])`)
	cleaned := re.ReplaceAllString(result.String(), "$1")
	return []byte(cleaned), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/devcontainer/... -run 'TestStripJSONC' -v`
Expected: all 3 tests PASS

**Step 5: Commit**

```bash
git add internal/devcontainer/devcontainer.go internal/devcontainer/devcontainer_test.go
git commit -m "feat: add JSONC parser and devcontainer struct"
```

---

### Task 2: Feature-to-package lookup and image inference

**Files:**
- Modify: `internal/devcontainer/devcontainer.go`
- Modify: `internal/devcontainer/devcontainer_test.go`

**Step 1: Write the failing tests**

Add to `internal/devcontainer/devcontainer_test.go`:

```go
func TestFeatureToPackages(t *testing.T) {
	features := map[string]json.RawMessage{
		"ghcr.io/devcontainers/features/node:1":       json.RawMessage(`{"version": "20"}`),
		"ghcr.io/devcontainers/features/go:1":         json.RawMessage(`{}`),
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
```

Note: add `"encoding/json"` to test imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run 'TestFeature|TestImage' -v`
Expected: compilation error — `FeatureToPackages` and `ImageToPackages` undefined

**Step 3: Write minimal implementation**

Add to `internal/devcontainer/devcontainer.go`:

```go
import (
	"github.com/hopboxdev/hopbox/internal/manifest"
)

// featureMap maps the short feature name (extracted from the ghcr.io URI) to a
// hopbox package definition. The version field is filled from the feature options
// at runtime if available.
var featureMap = map[string]manifest.Package{
	"node":        {Name: "nodejs", Backend: "nix"},
	"python":      {Name: "python3", Backend: "apt"},
	"go":          {Name: "go", Backend: "apt"},
	"rust":        {Name: "rustup", Backend: "apt"},
	"java":        {Name: "openjdk", Backend: "apt"},
	"git":         {Name: "git", Backend: "apt"},
	"github-cli":  {Name: "gh", Backend: "apt"},
}

// skipFeatures are features that don't map to hopbox packages.
var skipFeatures = map[string]bool{
	"common-utils":    true,
	"docker-in-docker": true,
	"docker-outside-of-docker": true,
}

// FeatureToPackages converts devcontainer features to hopbox packages.
// Returns packages and warnings for unmapped features.
func FeatureToPackages(features map[string]json.RawMessage) ([]manifest.Package, []string) {
	var pkgs []manifest.Package
	var warnings []string

	for uri := range features {
		name := featureName(uri)
		if skipFeatures[name] {
			continue
		}
		pkg, ok := featureMap[name]
		if !ok {
			warnings = append(warnings, "unknown feature "+uri)
			continue
		}
		// Extract version from feature options if present.
		var opts map[string]any
		if err := json.Unmarshal(features[uri], &opts); err == nil {
			if v, ok := opts["version"].(string); ok && v != "" && v != "latest" {
				pkg.Version = v
			}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, warnings
}

// featureName extracts the short name from a feature URI.
// "ghcr.io/devcontainers/features/node:1" → "node"
func featureName(uri string) string {
	// Strip version tag.
	if idx := strings.LastIndex(uri, ":"); idx > 0 {
		// Only strip if it looks like a version tag (after last /).
		slash := strings.LastIndex(uri, "/")
		if idx > slash {
			uri = uri[:idx]
		}
	}
	// Take last path segment.
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}

// imageMap maps devcontainer image path segments to hopbox packages.
var imageMap = map[string]string{
	"go":              "go",
	"python":          "python3",
	"typescript-node": "nodejs",
	"javascript-node": "nodejs",
	"node":            "nodejs",
	"rust":            "rustup",
	"java":            "openjdk",
	"dotnet":          "dotnet-sdk",
	"ruby":            "ruby",
	"php":             "php",
}

// ImageToPackages infers hopbox packages from a devcontainer image name.
func ImageToPackages(image string) ([]manifest.Package, string) {
	// Parse "mcr.microsoft.com/devcontainers/<lang>:<version>"
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return nil, "unknown image " + image
	}
	last := parts[len(parts)-1]
	// Split name:version
	name := last
	version := ""
	if idx := strings.Index(last, ":"); idx > 0 {
		name = last[:idx]
		version = last[idx+1:]
	}

	pkgName, ok := imageMap[name]
	if !ok {
		return nil, "unknown image " + image
	}

	pkg := manifest.Package{Name: pkgName, Backend: "apt"}
	// Only set version for images where the tag is a meaningful runtime version.
	// Skip generic tags like "ubuntu", "bookworm", "latest".
	if version != "" && !isOSTag(version) {
		pkg.Version = version
	}
	return []manifest.Package{pkg}, ""
}

func isOSTag(tag string) bool {
	osTags := []string{"ubuntu", "bookworm", "bullseye", "focal", "jammy", "noble", "latest", "lts"}
	for _, t := range osTags {
		if strings.HasPrefix(tag, t) {
			return true
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/devcontainer/... -run 'TestFeature|TestImage' -v`
Expected: all 4 tests PASS

**Step 5: Commit**

```bash
git add internal/devcontainer/devcontainer.go internal/devcontainer/devcontainer_test.go
git commit -m "feat: add feature-to-package lookup and image inference"
```

---

### Task 3: Docker Compose parsing

**Files:**
- Modify: `internal/devcontainer/devcontainer.go`
- Modify: `internal/devcontainer/devcontainer_test.go`

**Step 1: Write the failing tests**

Add to `internal/devcontainer/devcontainer_test.go`:

```go
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
```

Note: add `"os"`, `"path/filepath"` to test imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run 'TestParseCompose' -v`
Expected: compilation error — `ParseComposeFile` undefined

**Step 3: Write minimal implementation**

Add to `internal/devcontainer/devcontainer.go`:

```go
import (
	"os"
	"gopkg.in/yaml.v3"
	"github.com/hopboxdev/hopbox/internal/manifest"
)

// composeFile is the subset of docker-compose.yml we parse.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Ports       []string          `yaml:"ports"`
	Environment composeEnv        `yaml:"environment"`
	Volumes     []string          `yaml:"volumes"`
	DependsOn   composeDependsOn  `yaml:"depends_on"`
}

// composeEnv handles both map and list formats for environment.
type composeEnv map[string]string

func (e *composeEnv) UnmarshalYAML(value *yaml.Node) error {
	// Try map first.
	m := make(map[string]string)
	if err := value.Decode(&m); err == nil {
		*e = m
		return nil
	}
	// Try list of "KEY=VAL".
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*e = make(map[string]string, len(list))
	for _, item := range list {
		k, v, _ := strings.Cut(item, "=")
		(*e)[k] = v
	}
	return nil
}

// composeDependsOn handles both list and map formats.
type composeDependsOn []string

func (d *composeDependsOn) UnmarshalYAML(value *yaml.Node) error {
	// Try list first.
	var list []string
	if err := value.Decode(&list); err == nil {
		*d = list
		return nil
	}
	// Map format: {svc: {condition: ...}}.
	var m map[string]any
	if err := value.Decode(&m); err != nil {
		return err
	}
	for k := range m {
		*d = append(*d, k)
	}
	return nil
}

// ParseComposeFile reads a docker-compose YAML and maps services to hopbox service definitions.
func ParseComposeFile(path string) (map[string]manifest.Service, []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{"compose file not found: " + path}
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, []string{"parse compose file: " + err.Error()}
	}

	services := make(map[string]manifest.Service, len(cf.Services))
	var warnings []string

	for name, svc := range cf.Services {
		s := manifest.Service{
			Type:      "docker",
			Image:     svc.Image,
			Ports:     svc.Ports,
			Env:       map[string]string(svc.Environment),
			DependsOn: []string(svc.DependsOn),
		}

		// Map named volumes to data mounts where possible.
		for _, vol := range svc.Volumes {
			parts := strings.SplitN(vol, ":", 2)
			if len(parts) == 2 && !strings.HasPrefix(parts[0], "/") && !strings.HasPrefix(parts[0], ".") {
				// Named volume — use name as host path hint.
				s.Data = append(s.Data, manifest.DataMount{
					Host:      parts[0],
					Container: parts[1],
				})
			} else if len(parts) == 2 {
				s.Data = append(s.Data, manifest.DataMount{
					Host:      parts[0],
					Container: parts[1],
				})
			} else {
				warnings = append(warnings, "service "+name+": cannot map volume "+vol)
			}
		}

		services[name] = s
	}

	return services, warnings
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/devcontainer/... -run 'TestParseCompose' -v`
Expected: all 2 tests PASS

**Step 5: Commit**

```bash
git add internal/devcontainer/devcontainer.go internal/devcontainer/devcontainer_test.go
git commit -m "feat: add Docker Compose file parser for devcontainer import"
```

---

### Task 4: Convert function (end-to-end)

**Files:**
- Modify: `internal/devcontainer/devcontainer.go`
- Modify: `internal/devcontainer/devcontainer_test.go`

**Step 1: Write the failing tests**

Add to `internal/devcontainer/devcontainer_test.go`:

```go
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
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}

	dc := `{
		"name": "withdb",
		"dockerComposeFile": "docker-compose.yml",
		"containerEnv": {"DB_HOST": "localhost"}
	}`
	// devcontainer.json is inside a .devcontainer/ dir; compose path is relative to it.
	dcDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Put compose in same dir as devcontainer.json for this test.
	if err := os.WriteFile(filepath.Join(dcDir, "docker-compose.yml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run 'TestConvert' -v`
Expected: compilation error — `devcontainer.Convert` undefined

**Step 3: Write minimal implementation**

Add to `internal/devcontainer/devcontainer.go`:

```go
import (
	"path/filepath"
)

// vscodeCustomization extracts VS Code extensions from customizations.
type vscodeCustomization struct {
	Extensions []string `json:"extensions"`
}

// Convert reads a devcontainer.json file and returns a hopbox Workspace.
// Warnings list unmapped or partially-mapped fields.
func Convert(path string) (*manifest.Workspace, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	clean, err := StripJSONC(data)
	if err != nil {
		return nil, nil, err
	}

	var dc devcontainerJSON
	if err := json.Unmarshal(clean, &dc); err != nil {
		return nil, nil, fmt.Errorf("parse devcontainer.json: %w", err)
	}

	var warnings []string
	ws := &manifest.Workspace{
		Name: dc.Name,
	}

	// Image → inferred packages.
	if dc.Image != "" {
		pkgs, warn := ImageToPackages(dc.Image)
		ws.Packages = append(ws.Packages, pkgs...)
		if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Features → packages.
	if len(dc.Features) > 0 {
		pkgs, warns := FeatureToPackages(dc.Features)
		ws.Packages = append(ws.Packages, pkgs...)
		warnings = append(warnings, warns...)
	}

	// containerEnv → env.
	if len(dc.ContainerEnv) > 0 {
		ws.Env = dc.ContainerEnv
	}

	// postCreateCommand → scripts.setup.
	// postStartCommand → scripts.start.
	if len(dc.PostCreateCommand) > 0 || len(dc.PostStartCommand) > 0 {
		ws.Scripts = make(map[string]string)
		if s := dc.PostCreateCommand.String(); s != "" {
			ws.Scripts["setup"] = s
		}
		if s := dc.PostStartCommand.String(); s != "" {
			ws.Scripts["start"] = s
		}
	}

	// customizations.vscode.extensions → editor.extensions.
	if raw, ok := dc.Customizations["vscode"]; ok {
		var vsc vscodeCustomization
		if err := json.Unmarshal(raw, &vsc); err == nil && len(vsc.Extensions) > 0 {
			ws.Editor = &manifest.EditorConfig{
				Type:       "vscode-remote",
				Extensions: vsc.Extensions,
			}
		}
	}

	// dockerComposeFile → services.
	if len(dc.DockerComposeFile) > 0 {
		dcDir := filepath.Dir(path)
		composePath := filepath.Join(dcDir, dc.DockerComposeFile[0])
		services, warns := ParseComposeFile(composePath)
		if len(services) > 0 {
			ws.Services = services
		}
		warnings = append(warnings, warns...)
	}

	// Warn about unmapped fields.
	if dc.RemoteUser != "" {
		warnings = append(warnings, "remoteUser not mapped (hopbox runs as root on VPS)")
	}
	if len(dc.Mounts) > 0 {
		warnings = append(warnings, "mounts not mapped — configure manually in hopbox.yaml")
	}
	if dc.Build != nil {
		warnings = append(warnings, "build/Dockerfile not supported — use packages instead")
	}
	if len(dc.RunArgs) > 0 {
		warnings = append(warnings, "runArgs not mapped")
	}

	return ws, warnings, nil
}
```

Note: add `"fmt"` to imports.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/devcontainer/... -run 'TestConvert' -v`
Expected: all 5 tests PASS

**Step 5: Run full test suite**

Run: `go test ./internal/devcontainer/... -v`
Expected: all tests PASS (JSONC + feature + compose + convert)

**Step 6: Commit**

```bash
git add internal/devcontainer/devcontainer.go internal/devcontainer/devcontainer_test.go
git commit -m "feat: add Convert function for devcontainer.json import"
```

---

### Task 5: Wire into hop init --from

**Files:**
- Modify: `cmd/hop/init.go:1-35`

**Step 1: Update InitCmd to accept --from flag**

Replace the full contents of `cmd/hop/init.go`:

```go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hopboxdev/hopbox/internal/devcontainer"
	"gopkg.in/yaml.v3"
)

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct {
	From string `short:"f" help:"Import from devcontainer.json."`
}

func (c *InitCmd) Run() error {
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}

	if c.From != "" {
		return c.importDevcontainer(path)
	}
	return c.scaffold(path)
}

func (c *InitCmd) importDevcontainer(outPath string) error {
	ws, warnings, err := devcontainer.Convert(c.From)
	if err != nil {
		return fmt.Errorf("import devcontainer: %w", err)
	}

	data, err := yaml.Marshal(ws)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	// Prepend warnings as comments.
	var header strings.Builder
	header.WriteString("# Generated from " + c.From + "\n")
	if len(warnings) > 0 {
		header.WriteString("#\n# Warnings (may need manual attention):\n")
		for _, w := range warnings {
			header.WriteString("#   - " + w + "\n")
		}
	}
	header.WriteString("\n")

	output := header.String() + string(data)

	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		return err
	}

	fmt.Printf("Created hopbox.yaml from %s\n", c.From)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
	return nil
}

func (c *InitCmd) scaffold(outPath string) error {
	scaffold := `name: myapp
host: ""

services:
  app:
    type: docker
    image: myapp:latest
    ports: [8080]

bridges:
  - type: clipboard
  # - type: xdg-open
  # - type: notifications

session:
  manager: zellij
  name: myapp
`
	return os.WriteFile(outPath, []byte(scaffold), 0644)
}
```

**Step 2: Build**

Run: `make build`
Expected: clean build

**Step 3: Smoke test**

Create a temp devcontainer.json and test the command:

```bash
mkdir /tmp/test-dc && cd /tmp/test-dc
cat > devcontainer.json <<'EOF'
{
  "name": "test",
  "image": "mcr.microsoft.com/devcontainers/go:1.22",
  "features": {
    "ghcr.io/devcontainers/features/node:1": {"version": "20"}
  },
  "containerEnv": {"DEBUG": "1"},
  "postCreateCommand": "go mod download"
}
EOF
/path/to/dist/hop init --from devcontainer.json
cat hopbox.yaml
cd - && rm -rf /tmp/test-dc
```

Expected: `hopbox.yaml` generated with go and nodejs packages, env, scripts.

**Step 4: Run all tests**

Run: `go test ./... -count=1`
Expected: all PASS

**Step 5: Commit**

```bash
git add cmd/hop/init.go
git commit -m "feat: wire devcontainer import into hop init --from"
```

---

### Task 6: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark devcontainer compatibility as complete**

Change: `- [ ] devcontainer.json compatibility (read-only import)`
To: `- [x] devcontainer.json compatibility (read-only import)`

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark devcontainer import as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `internal/devcontainer/devcontainer.go` | Create — JSONC parser, feature/image lookup, compose parser, Convert |
| `internal/devcontainer/devcontainer_test.go` | Create — full test coverage |
| `cmd/hop/init.go` | Modify — add `--from` flag, import logic |
| `ROADMAP.md` | Modify — check off devcontainer item |
