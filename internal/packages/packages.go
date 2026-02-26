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
