# Static Package Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the `static` package backend so users can declare standalone binaries (from GitHub releases, etc.) in `hopbox.yaml` and have the agent download, verify, extract, and install them to `/opt/hopbox/bin/`.

**Architecture:** Add `staticInstall()` and `staticIsInstalled()` alongside the existing `aptInstall`/`nixInstall` in `internal/packages/packages.go`. Add `URL` and `SHA256` fields to the manifest and packages `Package` structs. Prepend `/opt/hopbox/bin` to the agent's PATH at startup. Users who SSH in can add it to their shell config themselves.

**Tech Stack:** Go stdlib (`net/http`, `archive/tar`, `archive/zip`, `compress/gzip`, `crypto/sha256`), `net/http/httptest` for test server.

---

### Task 1: Add URL and SHA256 fields to Package structs

**Files:**
- Modify: `internal/manifest/manifest.go:26-30`
- Modify: `internal/packages/packages.go:12-16`

**Step 1: Add fields to manifest.Package**

In `internal/manifest/manifest.go`, update the `Package` struct:

```go
type Package struct {
	Name    string `yaml:"name"`
	Backend string `yaml:"backend,omitempty"` // "nix", "apt", "static"
	Version string `yaml:"version,omitempty"`
	URL     string `yaml:"url,omitempty"`    // download URL (required for static)
	SHA256  string `yaml:"sha256,omitempty"` // optional hex-encoded SHA256
}
```

**Step 2: Add fields to packages.Package**

In `internal/packages/packages.go`, update the `Package` struct:

```go
type Package struct {
	Name    string `json:"name"`
	Backend string `json:"backend,omitempty"` // "apt", "nix", "static"
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`    // download URL (required for static)
	SHA256  string `json:"sha256,omitempty"` // optional hex-encoded SHA256
}
```

**Step 3: Add manifest validation for static packages**

In `internal/manifest/manifest.go`, add to `Validate()`:

```go
for _, pkg := range w.Packages {
	if pkg.Backend == "static" && pkg.URL == "" {
		return fmt.Errorf("package %q: url is required for static backend", pkg.Name)
	}
}
```

**Step 4: Commit**

```
feat: add URL and SHA256 fields to Package structs
```

---

### Task 2: Add manifest validation tests

**Files:**
- Modify: `internal/manifest/manifest_test.go`

**Step 1: Write test for static package with URL (valid)**

```go
func TestValidateStaticPackageWithURL(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`
name: test
packages:
  - name: ripgrep
    backend: static
    url: https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz
`))
	if err != nil {
		t.Errorf("expected valid manifest, got: %v", err)
	}
}
```

**Step 2: Write test for static package missing URL (invalid)**

```go
func TestValidateStaticPackageMissingURL(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`
name: test
packages:
  - name: ripgrep
    backend: static
`))
	if err == nil {
		t.Error("expected error for static package without url")
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/manifest/...`
Expected: PASS

**Step 4: Commit**

```
test: add manifest validation tests for static packages
```

---

### Task 3: Implement staticInstall — download, verify, extract, install

**Files:**
- Modify: `internal/packages/packages.go`

This is the core task. Add these functions to `packages.go`:

**Step 1: Add imports**

Add to the import block:

```go
"archive/tar"
"archive/zip"
"compress/gzip"
"crypto/sha256"
"encoding/hex"
"io"
"net/http"
"os"
"path/filepath"
```

**Step 2: Add the install path variable**

```go
// StaticBinDir is where static packages are installed. Variable for testing.
var StaticBinDir = "/opt/hopbox/bin"
```

**Step 3: Wire static into the Install and IsInstalled switch**

Replace the `"static"` case in `Install`:

```go
case "static":
	return staticInstall(ctx, pkg)
```

Add `"static"` case in `IsInstalled`:

```go
case "static":
	return staticIsInstalled(pkg), nil
```

**Step 4: Implement staticIsInstalled**

```go
func staticIsInstalled(pkg Package) bool {
	info, err := os.Stat(filepath.Join(StaticBinDir, pkg.Name))
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}
```

**Step 5: Implement staticInstall**

```go
func staticInstall(ctx context.Context, pkg Package) error {
	if pkg.URL == "" {
		return fmt.Errorf("static package %q: url is required", pkg.Name)
	}

	// Download to temp file.
	tmpFile, err := downloadToTemp(ctx, pkg.URL)
	if err != nil {
		return fmt.Errorf("download %q: %w", pkg.Name, err)
	}
	defer os.Remove(tmpFile)

	// Verify checksum if provided.
	if pkg.SHA256 != "" {
		if err := verifySHA256(tmpFile, pkg.SHA256); err != nil {
			return fmt.Errorf("verify %q: %w", pkg.Name, err)
		}
	}

	// Extract or use raw binary.
	binPath, cleanup, err := extractBinary(tmpFile, pkg.Name)
	if err != nil {
		return fmt.Errorf("extract %q: %w", pkg.Name, err)
	}
	defer cleanup()

	// Install to /opt/hopbox/bin/<name>.
	if err := os.MkdirAll(StaticBinDir, 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	dest := filepath.Join(StaticBinDir, pkg.Name)
	if err := copyFile(binPath, dest, 0755); err != nil {
		return fmt.Errorf("install %q: %w", pkg.Name, err)
	}

	return nil
}
```

**Step 6: Implement downloadToTemp**

```go
func downloadToTemp(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "hopbox-static-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
```

**Step 7: Implement verifySHA256**

```go
func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
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
```

**Step 8: Implement extractBinary**

Detects archive format from the URL/filename, extracts, finds the right executable.

```go
func extractBinary(archivePath, name string) (binPath string, cleanup func(), err error) {
	// Sniff the file header to detect archive format.
	f, err := os.Open(archivePath)
	if err != nil {
		return "", nil, err
	}
	header := make([]byte, 4)
	_, _ = io.ReadFull(f, header)
	f.Close()

	tmpDir, err := os.MkdirTemp("", "hopbox-extract-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	switch {
	case header[0] == 0x1f && header[1] == 0x8b: // gzip magic
		err = extractTarGz(archivePath, tmpDir)
	case header[0] == 'P' && header[1] == 'K': // zip magic
		err = extractZip(archivePath, tmpDir)
	default:
		// Treat as raw binary.
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

	bin, err := findExecutable(tmpDir, name)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return bin, cleanup, nil
}
```

**Step 9: Implement extractTarGz**

```go
func extractTarGz(path, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

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
		// Prevent path traversal.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}
```

**Step 10: Implement extractZip**

```go
func extractZip(path, destDir string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

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
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
```

**Step 11: Implement findExecutable**

```go
func findExecutable(dir, name string) (string, error) {
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

	// Prefer exact name match.
	for _, exe := range executables {
		if filepath.Base(exe) == name {
			return exe, nil
		}
	}

	// Fall back to sole executable.
	if len(executables) == 1 {
		return executables[0], nil
	}

	names := make([]string, len(executables))
	for i, e := range executables {
		names[i] = filepath.Base(e)
	}
	return "", fmt.Errorf("multiple executables found (%s), none matching %q", strings.Join(names, ", "), name)
}
```

**Step 12: Implement copyFile helper**

```go
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(perm)
}
```

**Step 13: Commit**

```
feat: implement static package backend
```

---

### Task 4: Add tests for static backend

**Files:**
- Modify: `internal/packages/packages_test.go`

Use `httptest.NewServer` to serve test archives. Build real tar.gz and zip archives in-memory for the test.

**Step 1: Add helper to create a test tar.gz with a named executable**

```go
func createTestTarGz(t *testing.T, dir, archiveName, binaryName string) (path string, sha256hex string) {
	t.Helper()
	archivePath := filepath.Join(dir, archiveName)
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	mw := io.MultiWriter(f, h)
	gw := gzip.NewWriter(mw)
	tw := tar.NewWriter(gw)

	// Add a subdirectory (like GitHub releases often do).
	_ = tw.WriteHeader(&tar.Header{
		Name:     "tool-v1.0/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	})

	content := []byte("#!/bin/sh\necho hello\n")
	_ = tw.WriteHeader(&tar.Header{
		Name:     "tool-v1.0/" + binaryName,
		Size:     int64(len(content)),
		Mode:     0755,
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(content)

	tw.Close()
	gw.Close()
	f.Close()

	return archivePath, hex.EncodeToString(h.Sum(nil))
}
```

**Step 2: Write test for static install with tar.gz**

```go
func TestInstall_StaticTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	// Override StaticBinDir for test — use a package-level variable or pass via options.
	// Since StaticBinDir is a const, we'll test the lower-level functions instead.

	archivePath, sha256hex := createTestTarGz(t, tmpDir, "tool.tar.gz", "mytool")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	// We need to test the full flow. To avoid writing to /opt/hopbox/bin,
	// we test the internal functions or use a configurable install dir.
}
```

Since `StaticBinDir` is a `var` (declared in Task 3), tests can override it:


```go
func TestInstall_StaticTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "opt", "hopbox", "bin")
	profileDir := filepath.Join(tmpDir, "etc", "profile.d")

	// Override install paths for test.
	origBinDir := packages.StaticBinDir
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = origBinDir })

	archivePath, sha256hex := createTestTarGz(t, tmpDir, "tool.tar.gz", "mytool")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	err := packages.Install(context.Background(), packages.Package{
		Name:    "mytool",
		Backend: "static",
		URL:     ts.URL + "/tool.tar.gz",
		SHA256:  sha256hex,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify binary exists and is executable.
	info, err := os.Stat(filepath.Join(binDir, "mytool"))
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("binary is not executable")
	}
}
```

**Step 3: Write test for SHA256 mismatch**

```go
func TestInstall_StaticSHA256Mismatch(t *testing.T) {
	tmpDir := t.TempDir()
	packages.StaticBinDir = filepath.Join(tmpDir, "bin")
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	archivePath, _ := createTestTarGz(t, tmpDir, "tool.tar.gz", "mytool")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	err := packages.Install(context.Background(), packages.Package{
		Name:    "mytool",
		Backend: "static",
		URL:     ts.URL + "/tool.tar.gz",
		SHA256:  "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Error("expected SHA256 mismatch error")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("error = %q, want sha256 mismatch", err)
	}
}
```

**Step 4: Write test for raw binary (no archive)**

```go
func TestInstall_StaticRawBinary(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	binaryContent := []byte("#!/bin/sh\necho hello\n")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	}))
	defer ts.Close()

	err := packages.Install(context.Background(), packages.Package{
		Name:    "mytool",
		Backend: "static",
		URL:     ts.URL + "/mytool",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, err := os.ReadFile(filepath.Join(binDir, "mytool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(binaryContent) {
		t.Error("installed binary content doesn't match")
	}
}
```

**Step 5: Write test for staticIsInstalled**

```go
func TestIsInstalled_Static(t *testing.T) {
	tmpDir := t.TempDir()
	packages.StaticBinDir = tmpDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Not installed yet.
	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "mytool", Backend: "static"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected not installed")
	}

	// Create the binary.
	if err := os.WriteFile(filepath.Join(tmpDir, "mytool"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	ok, err = packages.IsInstalled(context.Background(), packages.Package{Name: "mytool", Backend: "static"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected installed")
	}
}
```

**Step 6: Update the old TestInstall_Static to expect success**

The existing test expects an error. Remove it — the new tests cover static install properly.

**Step 7: Run tests**

Run: `go test ./internal/packages/...`
Expected: PASS

**Step 8: Commit**

```
test: add static package backend tests
```

---

### Task 5: Prepend /opt/hopbox/bin to agent PATH

**Files:**
- Modify: `cmd/hop-agent/main.go:28` (start of `ServeCmd.Run`)

**Step 1: Add PATH prepend at agent startup**

Add at the very start of `ServeCmd.Run()`, before anything else:

```go
func (c *ServeCmd) Run() error {
	// Ensure static package binaries are on PATH for scripts and checks.
	os.Setenv("PATH", packages.StaticBinDir+":"+os.Getenv("PATH"))

	kp, err := loadOrGenerateKey()
	// ... rest unchanged
```

Add `"github.com/hopboxdev/hopbox/internal/packages"` to the imports.

**Step 2: Build**

Run: `go build ./cmd/hop-agent/...`
Expected: success

**Step 3: Commit**

```
feat: prepend /opt/hopbox/bin to agent PATH at startup
```

---

### Task 6: Wire URL and SHA256 through the RPC callsite

**Files:**
- Modify: `cmd/hop/up.go` (package install step, around line 468-487)

**Step 1: Update the RPC params to include url and sha256**

The current code converts packages to `map[string]string`. Add url and sha256:

```go
for _, p := range ws.Packages {
	pkgs = append(pkgs, map[string]string{
		"name":    p.Name,
		"backend": p.Backend,
		"version": p.Version,
		"url":     p.URL,
		"sha256":  p.SHA256,
	})
}
```

**Step 2: Build and verify**

Run: `go build ./cmd/hop/...`
Expected: success

**Step 3: Commit**

```
feat: pass url and sha256 through package install RPC
```

---

### Task 7: Full verification

**Step 1: Run all tests**

Run: `go test ./...`
Expected: all PASS

**Step 2: Cross-compile agent**

Run: `CGO_ENABLED=0 GOOS=linux go build -o /dev/null ./cmd/hop-agent/...`
Expected: success

**Step 3: Lint**

Run: `golangci-lint run`
Expected: 0 issues

**Step 4: Update ROADMAP.md**

Mark the static package backend as complete:

```
- [x] Static package backend — download binary from URL
```

**Step 5: Final commit**

```
docs: mark static package backend as complete
```
