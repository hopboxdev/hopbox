package setup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// UpgradeAgentSSH establishes an SSH connection for agent upgrade.
// Call this before the TUI starts so passphrase prompts work.
func UpgradeAgentSSH(ctx context.Context, cfg *hostconfig.HostConfig) (*ssh.Client, error) {
	return SSHConnect(ctx, cfg, cfg.SSHKeyPath)
}

// UpgradeAgentWithClient uploads a new hop-agent binary to the remote host and
// restarts the service using an already-connected SSH client.
// Use UpgradeAgentSSH to establish the connection first.
func UpgradeAgentWithClient(ctx context.Context, client *ssh.Client, cfg *hostconfig.HostConfig, out io.Writer, clientVersion string, onStep func(string)) error {
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if onStep != nil {
			onStep(msg)
		} else {
			_, _ = fmt.Fprintln(out, msg)
		}
	}

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
	if err := installAgent(ctx, client, out, clientVersion, onStep); err != nil {
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

	logf("Agent upgrade complete.")
	return nil
}

// UpgradeAgent uploads a new hop-agent binary to the remote host and restarts
// the service. It reuses the SSH credentials saved during `hop setup`.
// If clientVersion is non-empty and the agent already reports that version,
// the upgrade is skipped.
func UpgradeAgent(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer, clientVersion string, onStep func(string)) error {
	client, err := UpgradeAgentSSH(ctx, cfg)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer func() { _ = client.Close() }()

	return UpgradeAgentWithClient(ctx, client, cfg, out, clientVersion, onStep)
}
