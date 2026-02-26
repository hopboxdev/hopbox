# Package Backend Abstraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace duplicated switch-case dispatch in `internal/packages` with a `Backend` interface, typed `BackendType` constants, and per-backend files.

**Architecture:** Define `BackendType` (typed int with string marshaling) and `Backend` interface in `backend.go`. Move each backend's `Install`/`IsInstalled`/`Remove` into its own file as a struct implementing `Backend`. A registry `map[BackendType]Backend` replaces all switch statements. Public API stays identical so existing tests pass unchanged.

**Tech Stack:** Go, `encoding/json` for BackendType marshaling.

---

### Task 1: Create `backend.go` with BackendType and Backend interface

**Files:**
- Create: `internal/packages/backend.go`

**Step 1: Write `backend.go`**

```go
package packages

import (
	"encoding/json"
	"fmt"
	"context"
)

// BackendType identifies a package backend.
type BackendType int

const (
	// Apt is the default package backend (Debian/Ubuntu apt-get).
	Apt BackendType = iota
	// Nix uses the Nix package manager.
	Nix
	// Static downloads a binary from a URL.
	Static
)

var backendNames = map[BackendType]string{
	Apt:    "apt",
	Nix:    "nix",
	Static: "static",
}

var backendValues = map[string]BackendType{
	"":       Apt,
	"apt":    Apt,
	"nix":    Nix,
	"static": Static,
}

func (b BackendType) String() string {
	if s, ok := backendNames[b]; ok {
		return s
	}
	return fmt.Sprintf("BackendType(%d)", b)
}

// ParseBackendType parses a string into a BackendType.
// An empty string defaults to Apt.
func ParseBackendType(s string) (BackendType, error) {
	if b, ok := backendValues[s]; ok {
		return b, nil
	}
	return 0, fmt.Errorf("unknown package backend %q", s)
}

func (b BackendType) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

func (b *BackendType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseBackendType(s)
	if err != nil {
		return err
	}
	*b = parsed
	return nil
}

// Backend is the interface every package backend implements.
type Backend interface {
	Install(ctx context.Context, pkg Package) error
	IsInstalled(ctx context.Context, pkg Package) (bool, error)
	Remove(ctx context.Context, pkg Package) error
}

// backends is the registry of all available backends.
var backends = map[BackendType]Backend{}

// RegisterBackend adds a backend to the registry.
// Called from each backend file's init-like setup in initBackends.
func registerBackend(t BackendType, b Backend) {
	backends[t] = b
}

func init() {
	initBackends()
}
```

Note: `initBackends()` will be defined in a later task after all backends exist. For now, add a placeholder.

**Step 2: Verify it compiles**

Run: `go build ./internal/packages/...`
Expected: will fail because `initBackends` doesn't exist yet. That's OK — we'll add it in Task 5.

Actually, to keep things compiling at each step, add a temporary `initBackends` placeholder at the bottom of `backend.go`:

```go
// initBackends registers all built-in backends.
// Defined here temporarily; will be fleshed out when backend files are created.
func initBackends() {
	// Will be populated in Task 5.
}
```

Run: `go build ./internal/packages/...`
Expected: compiles (though tests may fail since dispatch hasn't switched yet)

**Step 3: Commit**

```bash
git add internal/packages/backend.go
git commit -m "refactor: add BackendType, Backend interface, and registry"
```

---

### Task 2: Create `apt.go` — apt backend

**Files:**
- Create: `internal/packages/apt.go`

**Step 1: Write `apt.go`**

Move `aptInstall`, `aptIsInstalled`, and `aptRemove` from `packages.go` into a new `aptBackend` struct.

```go
package packages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type aptBackend struct{}

func (aptBackend) Install(ctx context.Context, pkg Package) error {
	name := pkg.Name
	if pkg.Version != "" {
		name = fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
	}
	cmd := exec.CommandContext(ctx, "apt-get", "install", "-y", name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (aptBackend) IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", pkg.Name)
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), "install ok installed"), nil
}

func (aptBackend) Remove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "apt-get", "remove", "-y", pkg.Name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/packages/...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/packages/apt.go
git commit -m "refactor: extract apt backend into apt.go"
```

---

### Task 3: Create `nix.go` — nix backend

**Files:**
- Create: `internal/packages/nix.go`

**Step 1: Write `nix.go`**

```go
package packages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type nixBackend struct{}

func (nixBackend) Install(ctx context.Context, pkg Package) error {
	attr := pkg.Name
	if pkg.Version != "" {
		attr = fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
	}
	cmd := exec.CommandContext(ctx, "nix", "profile", "install", "nixpkgs#"+attr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix profile install %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (nixBackend) IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	cmd := exec.CommandContext(ctx, "nix", "profile", "list")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), pkg.Name), nil
}

func (nixBackend) Remove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "nix", "profile", "remove", "nixpkgs#"+pkg.Name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix profile remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/packages/...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/packages/nix.go
git commit -m "refactor: extract nix backend into nix.go"
```

---

### Task 4: Create `static.go` — static backend

**Files:**
- Create: `internal/packages/static.go`

**Step 1: Write `static.go`**

Move `staticInstall`, `staticIsInstalled`, `staticRemove`, and all helper functions (`downloadToTemp`, `verifySHA256`, `extractBinary`, `extractTarGz`, `extractTarEntry`, `extractZip`, `extractZipEntry`, `findExecutable`, `staticMetaDir`, `writeStaticMeta`, `readStaticMeta`, `copyFile`) from `packages.go` into `static.go`.

```go
package packages

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type staticBackend struct{}

func (staticBackend) Install(ctx context.Context, pkg Package) error {
	if pkg.URL == "" {
		return fmt.Errorf("static package %q: url is required", pkg.Name)
	}

	tmpFile, err := downloadToTemp(ctx, pkg.URL)
	if err != nil {
		return fmt.Errorf("download %q: %w", pkg.Name, err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	if pkg.SHA256 != "" {
		if err := verifySHA256(tmpFile, pkg.SHA256); err != nil {
			return fmt.Errorf("verify %q: %w", pkg.Name, err)
		}
	}

	binPath, cleanup, err := extractBinary(tmpFile, pkg.URL)
	if err != nil {
		return fmt.Errorf("extract %q: %w", pkg.Name, err)
	}
	defer cleanup()

	if err := os.MkdirAll(StaticBinDir, 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	binName := filepath.Base(binPath)
	dest := filepath.Join(StaticBinDir, binName)
	if err := copyFile(binPath, dest, 0755); err != nil {
		return fmt.Errorf("install %q: %w", pkg.Name, err)
	}

	if err := writeStaticMeta(pkg.Name, binName); err != nil {
		return fmt.Errorf("write metadata for %q: %w", pkg.Name, err)
	}

	return nil
}

func (staticBackend) IsInstalled(_ context.Context, pkg Package) (bool, error) {
	binName, err := readStaticMeta(pkg.Name)
	if err != nil {
		return false, nil
	}
	info, err := os.Stat(filepath.Join(StaticBinDir, binName))
	if err != nil {
		return false, nil
	}
	return info.Mode()&0111 != 0, nil
}

func (staticBackend) Remove(_ context.Context, pkg Package) error {
	binName, err := readStaticMeta(pkg.Name)
	if err != nil {
		return nil
	}
	_ = os.Remove(filepath.Join(StaticBinDir, binName))
	_ = os.Remove(filepath.Join(staticMetaDir(), pkg.Name))
	return nil
}

// --- helpers (moved from packages.go, unchanged) ---

func downloadToTemp(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "hopbox-static-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func extractBinary(archivePath, sourceURL string) (binPath string, cleanup func(), err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", nil, err
	}
	header := make([]byte, 4)
	_, _ = io.ReadFull(f, header)
	_ = f.Close()

	tmpDir, err := os.MkdirTemp("", "hopbox-extract-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	switch {
	case header[0] == 0x1f && header[1] == 0x8b:
		err = extractTarGz(archivePath, tmpDir)
	case header[0] == 'P' && header[1] == 'K':
		err = extractZip(archivePath, tmpDir)
	default:
		name := filepath.Base(sourceURL)
		dest := filepath.Join(tmpDir, name)
		if err := copyFile(archivePath, dest, 0755); err != nil {
			cleanup()
			return "", nil, err
		}
		return dest, cleanup, nil
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}

	bin, err := findExecutable(tmpDir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return bin, cleanup, nil
}

func extractTarGz(path, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			if err := extractTarEntry(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractTarEntry(target string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, r)
	if err2 := out.Close(); err == nil {
		err = err2
	}
	return err
}

func extractZip(path, destDir string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0755)
			continue
		}

		_ = os.MkdirAll(filepath.Dir(target), 0755)
		if err := extractZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, f.Mode())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if err2 := out.Close(); err == nil {
		err = err2
	}
	return err
}

func findExecutable(dir string) (string, error) {
	var executables []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if info.Mode()&0111 != 0 {
			executables = append(executables, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(executables) == 0 {
		return "", fmt.Errorf("no executable found in archive")
	}

	if len(executables) == 1 {
		return executables[0], nil
	}

	names := make([]string, len(executables))
	for i, e := range executables {
		names[i] = filepath.Base(e)
	}
	return "", fmt.Errorf("multiple executables found (%s); cannot determine which to install", strings.Join(names, ", "))
}

func staticMetaDir() string {
	return filepath.Join(StaticBinDir, ".pkg")
}

func writeStaticMeta(pkgName, binName string) error {
	dir := staticMetaDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, pkgName), []byte(binName), 0644)
}

func readStaticMeta(pkgName string) (string, error) {
	data, err := os.ReadFile(filepath.Join(staticMetaDir(), pkgName))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err2 := out.Close(); err == nil {
		err = err2
	}
	if err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/packages/...`
Expected: no errors (duplicate functions exist in both files momentarily — we'll remove old ones in Task 5)

Actually, this will fail because the functions are duplicated. We need to handle this differently. Instead of creating `static.go` with duplicates, we'll create all backend files in Tasks 2-4, then do the switchover in Task 5 (remove old code from `packages.go` + wire up registry). To avoid duplicate symbol errors during Tasks 2-4, use build tags or just do the full switchover atomically.

**Better approach:** Create `static.go` but the old functions still exist in `packages.go`. This will cause duplicate symbol errors. So we need to do the extraction and deletion together in one task.

**Revised approach for Tasks 2-4:** Just create the files. The actual switchover (delete old functions from `packages.go`, wire registry, update `Package.Backend` type) happens all at once in Task 5. To avoid compile errors in Tasks 2-4, we'll use a `_new` suffix temporarily.

**Even simpler:** Do all backend extractions + deletion + switchover in a single task. Let me restructure.

---

**REVISED PLAN — fewer, larger tasks to avoid intermediate compile errors:**

---

### Task 1: Create `backend.go` with BackendType, Backend interface, and registry

**Files:**
- Create: `internal/packages/backend.go`

**Step 1: Write `backend.go`**

```go
package packages

import (
	"context"
	"encoding/json"
	"fmt"
)

// BackendType identifies a package backend.
type BackendType int

const (
	// Apt is the default package backend (Debian/Ubuntu apt-get).
	Apt BackendType = iota
	// Nix uses the Nix package manager.
	Nix
	// Static downloads a binary from a URL.
	Static
)

var backendNames = map[BackendType]string{
	Apt:    "apt",
	Nix:    "nix",
	Static: "static",
}

var backendValues = map[string]BackendType{
	"":       Apt,
	"apt":    Apt,
	"nix":    Nix,
	"static": Static,
}

func (b BackendType) String() string {
	if s, ok := backendNames[b]; ok {
		return s
	}
	return fmt.Sprintf("BackendType(%d)", b)
}

// ParseBackendType parses a string into a BackendType.
// An empty string defaults to Apt.
func ParseBackendType(s string) (BackendType, error) {
	if b, ok := backendValues[s]; ok {
		return b, nil
	}
	return 0, fmt.Errorf("unknown package backend %q", s)
}

// MarshalJSON serializes BackendType as a string for backward-compatible JSON.
func (b BackendType) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

// UnmarshalJSON deserializes BackendType from a JSON string.
func (b *BackendType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseBackendType(s)
	if err != nil {
		return err
	}
	*b = parsed
	return nil
}

// Backend is the interface every package backend implements.
type Backend interface {
	Install(ctx context.Context, pkg Package) error
	IsInstalled(ctx context.Context, pkg Package) (bool, error)
	Remove(ctx context.Context, pkg Package) error
}

// backends is the registry of all available backends.
var backends = map[BackendType]Backend{
	Apt:    aptBackend{},
	Nix:    nixBackend{},
	Static: staticBackend{},
}

// lookupBackend returns the backend for the given type.
func lookupBackend(t BackendType) (Backend, error) {
	b, ok := backends[t]
	if !ok {
		return nil, fmt.Errorf("unknown package backend %q", t)
	}
	return b, nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/packages/...`
Expected: fails — `aptBackend`, `nixBackend`, `staticBackend` don't exist yet. That's expected; they're created in Task 2.

**Step 3: Commit** (skip until Task 2 — we'll commit together so the tree always compiles)

---

### Task 2: Extract backends into separate files and rewrite `packages.go`

This is the main switchover task. Done atomically so the code compiles at every commit boundary.

**Files:**
- Create: `internal/packages/backend.go` (from Task 1)
- Create: `internal/packages/apt.go`
- Create: `internal/packages/nix.go`
- Create: `internal/packages/static.go`
- Rewrite: `internal/packages/packages.go`

**Step 1: Write `backend.go`** (the code from Task 1 above)

**Step 2: Write `apt.go`**

```go
package packages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type aptBackend struct{}

func (aptBackend) Install(ctx context.Context, pkg Package) error {
	name := pkg.Name
	if pkg.Version != "" {
		name = fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
	}
	cmd := exec.CommandContext(ctx, "apt-get", "install", "-y", name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (aptBackend) IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", pkg.Name)
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), "install ok installed"), nil
}

func (aptBackend) Remove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "apt-get", "remove", "-y", pkg.Name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 3: Write `nix.go`**

```go
package packages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type nixBackend struct{}

func (nixBackend) Install(ctx context.Context, pkg Package) error {
	attr := pkg.Name
	if pkg.Version != "" {
		attr = fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
	}
	cmd := exec.CommandContext(ctx, "nix", "profile", "install", "nixpkgs#"+attr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix profile install %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (nixBackend) IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	cmd := exec.CommandContext(ctx, "nix", "profile", "list")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), pkg.Name), nil
}

func (nixBackend) Remove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "nix", "profile", "remove", "nixpkgs#"+pkg.Name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix profile remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

**Step 4: Write `static.go`**

Move `staticInstall`, `staticIsInstalled`, `staticRemove` and all helper functions from `packages.go`. The full code is listed in the earlier "Task 4" section of this plan (the `staticBackend` struct plus `downloadToTemp`, `verifySHA256`, `extractBinary`, `extractTarGz`, `extractTarEntry`, `extractZip`, `extractZipEntry`, `findExecutable`, `staticMetaDir`, `writeStaticMeta`, `readStaticMeta`, `copyFile`). Use that code exactly.

**Step 5: Rewrite `packages.go`**

Replace the entire contents of `packages.go` with:

```go
// Package packages provides backends for installing system packages.
package packages

import (
	"context"
	"fmt"
	"log/slog"
)

// StaticBinDir is where static packages are installed. Variable for testing.
var StaticBinDir = "/opt/hopbox/bin"

// Package describes a system package to install.
type Package struct {
	Name    string      `json:"name"`
	Backend BackendType `json:"backend,omitempty"`
	Version string      `json:"version,omitempty"`
	URL     string      `json:"url,omitempty"`    // download URL (required for static)
	SHA256  string      `json:"sha256,omitempty"` // optional hex-encoded SHA256
}

// Install installs pkg using the appropriate backend.
func Install(ctx context.Context, pkg Package) error {
	b, err := lookupBackend(pkg.Backend)
	if err != nil {
		return err
	}
	return b.Install(ctx, pkg)
}

// IsInstalled checks whether a package is already installed.
func IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	b, err := lookupBackend(pkg.Backend)
	if err != nil {
		return false, err
	}
	return b.IsInstalled(ctx, pkg)
}

// Remove removes pkg using the appropriate backend.
func Remove(ctx context.Context, pkg Package) error {
	b, err := lookupBackend(pkg.Backend)
	if err != nil {
		return err
	}
	return b.Remove(ctx, pkg)
}

// Reconcile compares the desired package list against the state file and
// installs new packages, removes stale ones, and updates the state file.
// Errors from individual installs/removes are logged but do not stop the process.
func Reconcile(ctx context.Context, statePath string, desired []Package) error {
	previous, err := LoadState(statePath)
	if err != nil {
		slog.Warn("load package state", "err", err)
		previous = nil
	}

	prevMap := make(map[string]Package, len(previous))
	for _, p := range previous {
		prevMap[pkgKey(p)] = p
	}

	desiredMap := make(map[string]Package, len(desired))
	for _, p := range desired {
		desiredMap[pkgKey(p)] = p
	}

	// Remove stale packages (in previous but not in desired).
	for key, pkg := range prevMap {
		if _, ok := desiredMap[key]; !ok {
			slog.Info("removing stale package", "name", pkg.Name, "backend", pkg.Backend)
			if err := Remove(ctx, pkg); err != nil {
				slog.Warn("remove package", "name", pkg.Name, "err", err)
			}
		}
	}

	// Install new packages (in desired but not in previous).
	var installed []Package
	for _, pkg := range desired {
		if _, ok := prevMap[pkgKey(pkg)]; ok {
			installed = append(installed, pkg) // unchanged, keep in state
			continue
		}
		slog.Info("installing package", "name", pkg.Name, "backend", pkg.Backend)
		if err := Install(ctx, pkg); err != nil {
			slog.Warn("install package", "name", pkg.Name, "err", err)
			continue
		}
		installed = append(installed, pkg)
	}

	if err := SaveState(statePath, installed); err != nil {
		slog.Warn("save package state", "err", err)
	}
	return nil
}

// pkgKey returns a unique key for a package based on name and backend.
func pkgKey(p Package) string {
	return fmt.Sprintf("%s:%s", p.Backend, p.Name)
}
```

**Step 6: Verify it compiles and tests pass**

Run: `go build ./internal/packages/... && go test ./internal/packages/... -v`
Expected: compiles, all tests pass

**Step 7: Commit**

```bash
git add internal/packages/backend.go internal/packages/apt.go internal/packages/nix.go internal/packages/static.go internal/packages/packages.go
git commit -m "refactor: extract package backends into interface + per-backend files"
```

---

### Task 3: Update agent to use BackendType at the conversion boundary

**Files:**
- Modify: `internal/agent/agent.go:86-97`

**Step 1: Update the manifest-to-package conversion**

Replace the conversion loop (lines ~88-97) from:

```go
		pkgs := make([]packages.Package, len(ws.Packages))
		for i, p := range ws.Packages {
			pkgs[i] = packages.Package{
				Name:    p.Name,
				Backend: p.Backend,
				Version: p.Version,
				URL:     p.URL,
				SHA256:  p.SHA256,
			}
		}
```

With:

```go
		pkgs := make([]packages.Package, len(ws.Packages))
		for i, p := range ws.Packages {
			bt, err := packages.ParseBackendType(p.Backend)
			if err != nil {
				slog.Warn("unknown package backend", "name", p.Name, "backend", p.Backend, "err", err)
				continue
			}
			pkgs[i] = packages.Package{
				Name:    p.Name,
				Backend: bt,
				Version: p.Version,
				URL:     p.URL,
				SHA256:  p.SHA256,
			}
		}
```

**Step 2: Verify it compiles and all tests pass**

Run: `go build ./... && go test ./... 2>&1 | tail -30`
Expected: compiles, all tests pass

**Step 3: Commit**

```bash
git add internal/agent/agent.go
git commit -m "refactor: use ParseBackendType at manifest conversion boundary"
```

---

### Task 4: Update the `packages.install` RPC handler

**Files:**
- Modify: `internal/agent/api.go:161-182`

The `rpcPackagesInstall` handler accepts `[]packages.Package` via JSON. Since `Package.Backend` is now `BackendType` with JSON marshaling, this should work automatically. But we need to verify.

**Step 1: Verify it compiles**

Run: `go build ./internal/agent/...`
Expected: no errors (the JSON unmarshaling uses `BackendType.UnmarshalJSON`)

**Step 2: Run all tests**

Run: `go test ./... 2>&1 | tail -30`
Expected: all pass

**Step 3: Commit** (only if any changes were needed; otherwise skip)

---

### Task 5: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark package management abstraction as partially complete**

Change: `- [ ] Package management abstraction — backend interface, lock file (\`hopbox.lock\`)`
To: `- [x] Package management abstraction — backend interface (lock file is a separate future item)`

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark package management abstraction as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `internal/packages/backend.go` | Create — BackendType, Backend interface, registry |
| `internal/packages/apt.go` | Create — aptBackend implementing Backend |
| `internal/packages/nix.go` | Create — nixBackend implementing Backend |
| `internal/packages/static.go` | Create — staticBackend + all helpers |
| `internal/packages/packages.go` | Rewrite — Package struct, dispatch via registry, Reconcile |
| `internal/packages/state.go` | Unchanged |
| `internal/packages/packages_test.go` | Unchanged |
| `internal/agent/agent.go` | Modify — ParseBackendType at conversion boundary |
| `internal/agent/api.go` | Verify — JSON unmarshal works with BackendType |
| `ROADMAP.md` | Modify — check off package abstraction |
