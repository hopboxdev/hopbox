// Package packages provides backends for installing system packages.
package packages

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Package describes a system package to install.
type Package struct {
	Name    string `json:"name"`
	Backend string `json:"backend,omitempty"` // "apt", "nix", "static"
	Version string `json:"version,omitempty"`
}

// Install installs pkg using the appropriate backend.
func Install(ctx context.Context, pkg Package) error {
	switch pkg.Backend {
	case "apt", "":
		return aptInstall(ctx, pkg)
	case "nix":
		return nixInstall(ctx, pkg)
	case "static":
		return fmt.Errorf("static package backend not yet implemented")
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
