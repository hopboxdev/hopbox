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
