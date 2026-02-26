package packages_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hopboxdev/hopbox/internal/packages"
)

// fakeBin writes an executable shell script at dir/name that records its
// arguments (one per line) to an .args file, prints stdout to its own stdout,
// and exits with exitCode. Returns the .args file path.
func fakeBin(t *testing.T, dir, name, stdout string, exitCode int) string {
	t.Helper()
	argsFile := filepath.Join(dir, name+".args")
	stdoutFile := filepath.Join(dir, name+".stdout")
	if err := os.WriteFile(stdoutFile, []byte(stdout), 0644); err != nil {
		t.Fatalf("write stdout file: %v", err)
	}
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %s\ncat %s\nexit %d\n",
		argsFile, stdoutFile, exitCode,
	)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
		t.Fatalf("write fake bin %q: %v", name, err)
	}
	return argsFile
}

// readArgs reads the args file written by fakeBin and returns the args as a
// space-joined string for easy comparison.
func readArgs(t *testing.T, argsFile string) string {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return strings.Join(parts, " ")
}

func TestInstall_AptDefault(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Install(context.Background(), packages.Package{Name: "curl"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readArgs(t, af); got != "install -y curl" {
		t.Errorf("apt-get args = %q, want %q", got, "install -y curl")
	}
}

func TestInstall_AptWithVersion(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := packages.Install(context.Background(), packages.Package{
		Name: "curl", Backend: "apt", Version: "7.81.0",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readArgs(t, af); got != "install -y curl=7.81.0" {
		t.Errorf("apt-get args = %q, want %q", got, "install -y curl=7.81.0")
	}
}

func TestInstall_Nix(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "nix", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Install(context.Background(), packages.Package{Name: "ripgrep", Backend: "nix"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readArgs(t, af); got != "profile install nixpkgs#ripgrep" {
		t.Errorf("nix args = %q, want %q", got, "profile install nixpkgs#ripgrep")
	}
}

func TestInstall_NixWithVersion(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "nix", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	err := packages.Install(context.Background(), packages.Package{
		Name: "ripgrep", Backend: "nix", Version: "13.0.0",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readArgs(t, af); got != "profile install nixpkgs#ripgrep@13.0.0" {
		t.Errorf("nix args = %q, want %q", got, "profile install nixpkgs#ripgrep@13.0.0")
	}
}

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

	_ = tw.Close()
	_ = gw.Close()
	_ = f.Close()

	return archivePath, hex.EncodeToString(h.Sum(nil))
}

func TestInstall_StaticTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "opt", "hopbox", "bin")

	origBinDir := packages.StaticBinDir
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = origBinDir })

	// Package name "ripgrep" but binary in archive is "rg".
	archivePath, sha256hex := createTestTarGz(t, tmpDir, "tool.tar.gz", "rg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	err := packages.Install(context.Background(), packages.Package{
		Name:    "ripgrep",
		Backend: "static",
		URL:     ts.URL + "/tool.tar.gz",
		SHA256:  sha256hex,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Binary should be installed under its archive name "rg", not "ripgrep".
	info, err := os.Stat(filepath.Join(binDir, "rg"))
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("binary is not executable")
	}

	// Package name should NOT exist as a binary.
	if _, err := os.Stat(filepath.Join(binDir, "ripgrep")); err == nil {
		t.Error("binary should not be installed under package name")
	}
}

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

func TestInstall_StaticRawBinary(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	binaryContent := []byte("#!/bin/sh\necho hello\n")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binaryContent)
	}))
	defer ts.Close()

	err := packages.Install(context.Background(), packages.Package{
		Name:    "My Tool",
		Backend: "static",
		URL:     ts.URL + "/mytool",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Binary name comes from URL path ("mytool"), not package name ("My Tool").
	installed, err := os.ReadFile(filepath.Join(binDir, "mytool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(binaryContent) {
		t.Error("installed binary content doesn't match")
	}
}

func TestIsInstalled_Static(t *testing.T) {
	tmpDir := t.TempDir()
	packages.StaticBinDir = tmpDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Not installed — no metadata, no binary.
	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "ripgrep", Backend: "static"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected not installed")
	}

	// Simulate a previous install: write metadata and binary.
	metaDir := filepath.Join(tmpDir, ".pkg")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "ripgrep"), []byte("rg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rg"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	ok, err = packages.IsInstalled(context.Background(), packages.Package{Name: "ripgrep", Backend: "static"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected installed")
	}
}

func TestInstall_UnknownBackend(t *testing.T) {
	err := packages.Install(context.Background(), packages.Package{Name: "tool", Backend: "brew"})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestRemove_Apt(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "curl", Backend: "apt"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "remove -y curl" {
		t.Errorf("apt-get args = %q, want %q", got, "remove -y curl")
	}
}

func TestRemove_AptDefault(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "curl"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "remove -y curl" {
		t.Errorf("apt-get args = %q, want %q", got, "remove -y curl")
	}
}

func TestRemove_Nix(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "nix", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "ripgrep", Backend: "nix"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "profile remove nixpkgs#ripgrep" {
		t.Errorf("nix args = %q, want %q", got, "profile remove nixpkgs#ripgrep")
	}
}

func TestRemove_Static(t *testing.T) {
	tmpDir := t.TempDir()
	packages.StaticBinDir = tmpDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Simulate a previously installed static package.
	metaDir := filepath.Join(tmpDir, ".pkg")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "ripgrep"), []byte("rg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rg"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := packages.Remove(context.Background(), packages.Package{Name: "ripgrep", Backend: "static"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Binary and metadata should be gone.
	if _, err := os.Stat(filepath.Join(tmpDir, "rg")); !os.IsNotExist(err) {
		t.Error("binary should be removed")
	}
	if _, err := os.Stat(filepath.Join(metaDir, "ripgrep")); !os.IsNotExist(err) {
		t.Error("metadata should be removed")
	}
}

func TestRemove_UnknownBackend(t *testing.T) {
	err := packages.Remove(context.Background(), packages.Package{Name: "tool", Backend: "brew"})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestInstall_AptError(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "apt-get", "", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Install(context.Background(), packages.Package{Name: "curl"}); err == nil {
		t.Error("expected error when apt-get exits non-zero")
	}
}

func TestIsInstalled_AptTrue(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "dpkg-query", "install ok installed", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "curl"})
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !ok {
		t.Error("IsInstalled = false, want true")
	}
}

func TestIsInstalled_AptFalse(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "dpkg-query", "", 1) // non-zero exit = not installed
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "curl"})
	if err != nil {
		t.Fatalf("IsInstalled returned unexpected error: %v", err)
	}
	if ok {
		t.Error("IsInstalled = true, want false")
	}
}

func TestIsInstalled_NixTrue(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "nix", "nixpkgs#ripgrep 13.0.0\n", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "ripgrep", Backend: "nix"})
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !ok {
		t.Error("IsInstalled = false, want true")
	}
}

func TestIsInstalled_NixFalse(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "nix", "nixpkgs#other-package\n", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	ok, err := packages.IsInstalled(context.Background(), packages.Package{Name: "ripgrep", Backend: "nix"})
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if ok {
		t.Error("IsInstalled = true, want false")
	}
}

func TestReconcile_FirstRun(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	desired := []packages.Package{
		{Name: "curl", Backend: "apt"},
	}

	if err := packages.Reconcile(context.Background(), statePath, desired); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// State file should now list curl.
	got, _ := packages.LoadState(statePath)
	if len(got) != 1 || got[0].Name != "curl" {
		t.Errorf("state = %+v, want [curl]", got)
	}
}

func TestReconcile_RemoveStale(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")

	// Pretend curl and htop were previously installed.
	prev := []packages.Package{
		{Name: "curl", Backend: "apt"},
		{Name: "htop", Backend: "apt"},
	}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// New manifest only has curl.
	desired := []packages.Package{
		{Name: "curl", Backend: "apt"},
	}
	if err := packages.Reconcile(context.Background(), statePath, desired); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, _ := packages.LoadState(statePath)
	if len(got) != 1 || got[0].Name != "curl" {
		t.Errorf("state = %+v, want [curl]", got)
	}
}

func TestReconcile_EmptyManifest(t *testing.T) {
	dir := t.TempDir()
	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	prev := []packages.Package{{Name: "curl", Backend: "apt"}}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// Empty manifest = remove everything.
	if err := packages.Reconcile(context.Background(), statePath, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, _ := packages.LoadState(statePath)
	if len(got) != 0 {
		t.Errorf("state = %+v, want empty", got)
	}
}

func TestReconcile_SkipUnchanged(t *testing.T) {
	dir := t.TempDir()
	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	// apt-get should NOT be called if packages are unchanged.
	// Use exit code 1 so the test fails if it's called.
	fakeBin(t, fakeDir, "apt-get", "", 1)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	pkgs := []packages.Package{{Name: "curl", Backend: "apt"}}
	if err := packages.SaveState(statePath, pkgs); err != nil {
		t.Fatal(err)
	}

	// Same list — nothing to install or remove.
	if err := packages.Reconcile(context.Background(), statePath, pkgs); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func TestReconcile_StaticRemove(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Simulate previously installed static package.
	metaDir := filepath.Join(binDir, ".pkg")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "ripgrep"), []byte("rg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "rg"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(dir, "state.json")
	prev := []packages.Package{{Name: "ripgrep", Backend: "static", URL: "https://example.com/rg.tar.gz"}}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// Empty manifest — ripgrep should be removed.
	if err := packages.Reconcile(context.Background(), statePath, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(binDir, "rg")); !os.IsNotExist(err) {
		t.Error("binary should be removed")
	}
	got, _ := packages.LoadState(statePath)
	if len(got) != 0 {
		t.Errorf("state = %+v, want empty", got)
	}
}
