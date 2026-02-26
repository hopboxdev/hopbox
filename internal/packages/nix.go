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
