// Package packages provides backends for installing system packages.
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
	"os/exec"
	"path/filepath"
	"strings"
)

// StaticBinDir is where static packages are installed. Variable for testing.
var StaticBinDir = "/opt/hopbox/bin"

// Package describes a system package to install.
type Package struct {
	Name    string `json:"name"`
	Backend string `json:"backend,omitempty"` // "apt", "nix", "static"
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`    // download URL (required for static)
	SHA256  string `json:"sha256,omitempty"` // optional hex-encoded SHA256
}

// Install installs pkg using the appropriate backend.
func Install(ctx context.Context, pkg Package) error {
	switch pkg.Backend {
	case "apt", "":
		return aptInstall(ctx, pkg)
	case "nix":
		return nixInstall(ctx, pkg)
	case "static":
		return staticInstall(ctx, pkg)
	default:
		return fmt.Errorf("unknown package backend %q", pkg.Backend)
	}
}

// IsInstalled checks whether a package is already installed.
// For apt this calls dpkg-query; for nix it calls nix profile list.
func IsInstalled(ctx context.Context, pkg Package) (bool, error) {
	switch pkg.Backend {
	case "apt", "":
		return aptIsInstalled(ctx, pkg.Name)
	case "nix":
		return nixIsInstalled(ctx, pkg.Name)
	case "static":
		return staticIsInstalled(pkg), nil
	default:
		return false, nil
	}
}

func aptInstall(ctx context.Context, pkg Package) error {
	name := pkg.Name
	if pkg.Version != "" {
		name = fmt.Sprintf("%s=%s", pkg.Name, pkg.Version)
	}
	// DEBIAN_FRONTEND=noninteractive suppresses interactive prompts.
	cmd := exec.CommandContext(ctx, "apt-get", "install", "-y", name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func aptIsInstalled(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", name)
	out, err := cmd.Output()
	if err != nil {
		return false, nil // not installed
	}
	return strings.Contains(string(out), "install ok installed"), nil
}

func nixInstall(ctx context.Context, pkg Package) error {
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

func nixIsInstalled(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "nix", "profile", "list")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), name), nil
}

func staticIsInstalled(pkg Package) bool {
	binName, err := readStaticMeta(pkg.Name)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(StaticBinDir, binName))
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

func staticInstall(ctx context.Context, pkg Package) error {
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
	case header[0] == 0x1f && header[1] == 0x8b: // gzip magic
		err = extractTarGz(archivePath, tmpDir)
	case header[0] == 'P' && header[1] == 'K': // zip magic
		err = extractZip(archivePath, tmpDir)
	default:
		// Raw binary — use the last path segment of the source URL as filename.
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
