package packages_test

import (
	"context"
	"fmt"
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

func TestInstall_Static(t *testing.T) {
	err := packages.Install(context.Background(), packages.Package{Name: "tool", Backend: "static"})
	if err == nil {
		t.Error("expected error for static backend")
	}
}

func TestInstall_UnknownBackend(t *testing.T) {
	err := packages.Install(context.Background(), packages.Package{Name: "tool", Backend: "brew"})
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
