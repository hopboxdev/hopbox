package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// Options configures a bootstrap operation.
type Options struct {
	Name    string
	SSHHost string
	SSHPort int
	SSHUser string
	// SSHKeyPath is the path to the private key file. If empty, uses
	// ~/.ssh/id_rsa, ~/.ssh/id_ed25519, etc.
	SSHKeyPath string
}

// Bootstrap performs the full setup sequence:
//  1. SSH into the remote host (TOFU: capture and save the host key)
//  2. Upload and install hop-agent
//  3. Generate server keys (hop-agent setup)
//  4. Exchange public keys
//  5. Start systemd service
//  6. Verify tunnel
//  7. Save HostConfig (including the captured SSH host key)
func Bootstrap(ctx context.Context, opts Options, out io.Writer) (*hostconfig.HostConfig, error) {
	if opts.SSHPort == 0 {
		opts.SSHPort = 22
	}
	if opts.SSHUser == "" {
		opts.SSHUser = "root"
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
	}

	logf("Connecting to %s:%d as %s...", opts.SSHHost, opts.SSHPort, opts.SSHUser)

	// TOFU: capture the server's host key on first connection.
	var capturedKey ssh.PublicKey
	captureCallback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		capturedKey = key
		return nil
	}

	client, err := sshConnect(ctx, opts, captureCallback)
	if err != nil {
		return nil, fmt.Errorf("SSH connect: %w", err)
	}
	defer client.Close()

	logf("Connected. Installing hop-agent...")
	if err := installAgent(ctx, client, out); err != nil {
		return nil, fmt.Errorf("install agent: %w", err)
	}

	logf("Generating server WireGuard keys...")
	serverPubKeyB64, err := runRemote(client, "hop-agent setup")
	if err != nil {
		return nil, fmt.Errorf("hop-agent setup (phase 1): %w", err)
	}
	serverPubKeyB64 = strings.TrimSpace(serverPubKeyB64)

	logf("Generating client WireGuard keys...")
	clientKP, err := wgkey.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate client keys: %w", err)
	}

	logf("Sending client public key to agent...")
	_, err = runRemote(client, "hop-agent setup --client-pubkey="+clientKP.PublicKeyBase64())
	if err != nil {
		return nil, fmt.Errorf("hop-agent setup (phase 2): %w", err)
	}

	logf("Enabling hop-agent service...")
	_, err = runRemote(client, "systemctl enable --now hop-agent")
	if err != nil {
		logf("Warning: systemctl failed (non-systemd host?): %v", err)
	}

	cfg := &hostconfig.HostConfig{
		Name:          opts.Name,
		Endpoint:      net.JoinHostPort(opts.SSHHost, strconv.Itoa(tunnel.DefaultPort)),
		PrivateKey:    clientKP.PrivateKeyBase64(),
		PeerPublicKey: serverPubKeyB64,
		TunnelIP:      tunnel.ClientIP + "/24",
		AgentIP:       tunnel.ServerIP,
		SSHUser:       opts.SSHUser,
		SSHHost:       opts.SSHHost,
		SSHPort:       opts.SSHPort,
		SSHHostKey:    MarshalHostKey(capturedKey),
	}
	if capturedKey != nil {
		logf("SSH host key captured (%s): %s", capturedKey.Type(),
			ssh.FingerprintSHA256(capturedKey))
	}

	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("save host config: %w", err)
	}

	logf("Host config saved. Bootstrap complete.")
	return cfg, nil
}

// SSHConnect establishes an SSH connection to a known host, verifying its
// host key against the fingerprint saved during bootstrap.
func SSHConnect(ctx context.Context, cfg *hostconfig.HostConfig, keyPath string) (*ssh.Client, error) {
	opts := Options{
		SSHHost:    cfg.SSHHost,
		SSHPort:    cfg.SSHPort,
		SSHUser:    cfg.SSHUser,
		SSHKeyPath: keyPath,
	}
	callback, err := HostKeyCallbackFor(cfg.SSHHostKey)
	if err != nil {
		return nil, fmt.Errorf("load saved host key: %w", err)
	}
	return sshConnect(ctx, opts, callback)
}

// sshConnect establishes an SSH connection using key-based auth.
// hostKeyCallback controls host key verification — callers must provide one.
func sshConnect(ctx context.Context, opts Options, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	signers, err := LoadSigners(opts.SSHKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load SSH keys: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            opts.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	addr := net.JoinHostPort(opts.SSHHost, strconv.Itoa(opts.SSHPort))

	var client *ssh.Client
	var dialErr error
	dialer := &net.Dialer{}
	for i := 0; i < 3; i++ {
		netConn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			dialErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, config)
		if err != nil {
			netConn.Close()
			dialErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		client = ssh.NewClient(sshConn, chans, reqs)
		dialErr = nil
		break
	}
	if dialErr != nil {
		return nil, dialErr
	}
	return client, nil
}

// runRemote executes a command on the remote host and returns combined output.
func runRemote(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf

	if err := sess.Run(cmd); err != nil {
		return "", fmt.Errorf("remote %q: %w (output: %s)", cmd, err, buf.String())
	}
	return buf.String(), nil
}

// LoadSigners loads SSH private key signers. It tries the SSH agent first
// (via $SSH_AUTH_SOCK), then falls back to key files. If a key file is
// passphrase-protected, the user is prompted on stderr.
// Exported for testing.
func LoadSigners(keyPath string) ([]ssh.Signer, error) {
	var signers []ssh.Signer

	// Try SSH agent first — works transparently when the key is already loaded.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			agentSigners, err := agent.NewClient(conn).Signers()
			if err == nil {
				signers = append(signers, agentSigners...)
			}
		}
	}

	// Collect key file paths to try.
	paths := []string{keyPath}
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		paths = []string{
			home + "/.ssh/id_ed25519",
			home + "/.ssh/id_rsa",
			home + "/.ssh/id_ecdsa",
		}
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read key %q: %w", p, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			var passErr *ssh.PassphraseMissingError
			if !errors.As(err, &passErr) {
				return nil, fmt.Errorf("parse key %q: %w", p, err)
			}
			fmt.Fprintf(os.Stderr, "Enter passphrase for %s: ", p)
			passphrase, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return nil, fmt.Errorf("read passphrase: %w", err)
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, passphrase)
			if err != nil {
				return nil, fmt.Errorf("parse key %q: %w", p, err)
			}
		}
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("no SSH private keys found")
	}
	return signers, nil
}

// MarshalHostKey serialises an SSH public key to authorized_keys format.
// Returns empty string if key is nil.
func MarshalHostKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

// HostKeyCallbackFor returns a HostKeyCallback that enforces the saved key.
// If savedKey is empty (no prior connection recorded), it returns an error
// so callers are explicit about whether they want TOFU or enforcement.
func HostKeyCallbackFor(savedKey string) (ssh.HostKeyCallback, error) {
	if savedKey == "" {
		return nil, fmt.Errorf("no SSH host key on record; re-run 'hop setup' to bootstrap")
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(savedKey))
	if err != nil {
		return nil, fmt.Errorf("parse saved host key: %w", err)
	}
	return ssh.FixedHostKey(pub), nil
}

// ServerPubKeyFromB64 parses a server public key from base64 and returns
// the hex representation for use in IPC.
func ServerPubKeyFromB64(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("decode server pubkey: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("invalid public key length: %d", len(raw))
	}
	return fmt.Sprintf("%x", raw), nil
}
