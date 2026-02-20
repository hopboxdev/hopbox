package setup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// UpgradeAgent uploads a new hop-agent binary to the remote host and restarts
// the service. It reuses the SSH credentials saved during `hop setup`.
// If clientVersion is non-empty and the agent already reports that version,
// the upgrade is skipped.
func UpgradeAgent(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer, clientVersion string) error {
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	logf("Connecting to %s:%d as %s...", cfg.SSHHost, cfg.SSHPort, cfg.SSHUser)

	client, err := SSHConnect(ctx, cfg, cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Show the currently running agent version and exit early if already current.
	// Output format: "hop-agent <VERSION> (commit <COMMIT>, built <DATE>)"
	if ver, err := runRemote(client, "hop-agent version"); err == nil {
		logf("Current agent version: %s", ver)
		if clientVersion != "" {
			// Extract just the version token (second word).
			fields := strings.Fields(ver)
			if len(fields) >= 2 && fields[1] == clientVersion {
				logf("Agent is already up to date.")
				return nil
			}
		}
	}

	logf("Uploading new hop-agent binary...")
	if err := installAgent(ctx, client, out, clientVersion); err != nil {
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
