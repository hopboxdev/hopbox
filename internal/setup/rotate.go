package setup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// RotateKeys rotates the WireGuard keypair for a registered host over SSH.
// It uses the SSH credentials saved during `hop setup` and does not require
// the WireGuard tunnel to be active.
//
// On the server side the old agent.key is backed up to agent.key.bak before
// the new keypair is written, providing a manual recovery path if the client
// config save fails.
func RotateKeys(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer) error {
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	logf("Connecting to %s:%d as %s...", cfg.SSHHost, cfg.SSHPort, cfg.SSHUser)

	client, err := SSHConnect(ctx, cfg, cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("SSH connect: %w", err)
	}
	defer func() { _ = client.Close() }()

	logf("Regenerating server WireGuard keys...")
	if _, err = runRemote(client, "sudo hop-agent rotate"); err != nil {
		return fmt.Errorf("hop-agent rotate: %w", err)
	}

	// Read new server public key from the file the agent just wrote.
	pubKeyLine, err := runRemote(client, "sudo grep '^public=' /etc/hopbox/agent.key")
	if err != nil {
		return fmt.Errorf("read agent public key: %w", err)
	}
	serverPubKeyB64 := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(pubKeyLine), "public="))
	if _, err = wgkey.KeyB64ToHex(serverPubKeyB64); err != nil {
		return fmt.Errorf("agent key file contains invalid public key %q: %w", serverPubKeyB64, err)
	}

	logf("Generating new client WireGuard keys...")
	clientKP, err := wgkey.Generate()
	if err != nil {
		return fmt.Errorf("generate client keys: %w", err)
	}

	logf("Sending new client public key to agent...")
	if _, err = runRemote(client, "sudo hop-agent setup --client-pubkey="+clientKP.PublicKeyBase64()); err != nil {
		return fmt.Errorf("hop-agent setup --client-pubkey: %w", err)
	}

	logf("Restarting hop-agent service...")
	if _, err = runRemote(client, "sudo systemctl restart hop-agent"); err != nil {
		logf("Warning: systemctl restart failed (non-systemd host?): %v", err)
	}

	cfg.PrivateKey = clientKP.PrivateKeyBase64()
	cfg.PeerPublicKey = serverPubKeyB64
	if err = cfg.Save(); err != nil {
		return fmt.Errorf("save host config: %w", err)
	}

	logf("Key rotation complete.")
	return nil
}
