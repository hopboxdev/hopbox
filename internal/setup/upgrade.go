package setup

import (
	"context"
	"fmt"
	"io"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// UpgradeAgent uploads a new hop-agent binary to the remote host and restarts
// the service. It reuses the SSH credentials saved during `hop setup`.
func UpgradeAgent(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer) error {
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	logf("Connecting to %s:%d as %s...", cfg.SSHHost, cfg.SSHPort, cfg.SSHUser)

	client, err := SSHConnect(ctx, cfg, cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Show the currently running agent version before upgrading.
	if ver, err := runRemote(client, "hop-agent version"); err == nil {
		logf("Current agent version: %s", ver)
	}

	logf("Uploading new hop-agent binary...")
	if err := installAgent(ctx, client, out); err != nil {
		return fmt.Errorf("install agent: %w", err)
	}

	logf("Restarting hop-agent service...")
	if _, err := runRemote(client, "sudo systemctl restart hop-agent"); err != nil {
		logf("Warning: systemctl restart failed (non-systemd host?): %v", err)
	}

	// Confirm the new version.
	if ver, err := runRemote(client, "hop-agent version"); err == nil {
		logf("New agent version: %s", ver)
	}

	logf("Upgrade complete.")
	return nil
}
