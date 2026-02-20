package setup_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
)

// TestRotateKeys drives RotateKeys() against an in-process mock SSH server.
// It verifies that:
//   - RotateKeys returns without error
//   - cfg.PrivateKey is replaced with a new (different) value
//   - cfg.PeerPublicKey is set to the server's newly generated public key
//   - The updated config is persisted to disk
func TestRotateKeys(t *testing.T) {
	// New server WireGuard public key that the mock returns after regeneration.
	rawPub := make([]byte, 32)
	if _, err := rand.Read(rawPub); err != nil {
		t.Fatalf("rand: %v", err)
	}
	newServerPubKeyB64 := base64.StdEncoding.EncodeToString(rawPub)

	// Start mock SSH server.
	hostKey, err := generateEd25519Key()
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serveMockSSHForRotate(t, conn, hostKey, newServerPubKeyB64)
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	// Write a client SSH private key to a temp file.
	keyFile := writeClientSSHKey(t)

	// Redirect HOME so cfg.Save() writes under a temp directory.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")

	// Build an initial HostConfig with dummy WireGuard keys.
	oldPrivRaw := make([]byte, 32)
	oldPubRaw := make([]byte, 32)
	if _, err := rand.Read(oldPrivRaw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if _, err := rand.Read(oldPubRaw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	oldPrivB64 := base64.StdEncoding.EncodeToString(oldPrivRaw)
	oldPubB64 := base64.StdEncoding.EncodeToString(oldPubRaw)

	cfg := &hostconfig.HostConfig{
		Name:          "testbox",
		Endpoint:      "127.0.0.1:51820",
		PrivateKey:    oldPrivB64,
		PeerPublicKey: oldPubB64,
		TunnelIP:      "10.10.0.1/24",
		AgentIP:       "10.10.0.2",
		SSHUser:       "testuser",
		SSHHost:       "127.0.0.1",
		SSHPort:       port,
		SSHKeyPath:    keyFile,
		SSHHostKey:    setup.MarshalHostKey(hostKey.PublicKey()),
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	if err := setup.RotateKeys(context.Background(), cfg, io.Discard); err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}

	// Client private key must have changed.
	if cfg.PrivateKey == oldPrivB64 {
		t.Error("cfg.PrivateKey was not updated")
	}
	// Server public key must match what the mock returned.
	if cfg.PeerPublicKey != newServerPubKeyB64 {
		t.Errorf("cfg.PeerPublicKey = %q, want %q", cfg.PeerPublicKey, newServerPubKeyB64)
	}

	// Saved config on disk must reflect the new keys.
	saved, err := hostconfig.Load("testbox")
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if saved.PrivateKey != cfg.PrivateKey {
		t.Errorf("saved PrivateKey = %q, want %q", saved.PrivateKey, cfg.PrivateKey)
	}
	if saved.PeerPublicKey != newServerPubKeyB64 {
		t.Errorf("saved PeerPublicKey = %q, want %q", saved.PeerPublicKey, newServerPubKeyB64)
	}
}

// serveMockSSHForRotate handles a single SSH connection for rotation,
// dispatching each session to handleMockSessionForRotate.
func serveMockSSHForRotate(t *testing.T, nc net.Conn, hostKey ssh.Signer, newPubKeyB64 string) {
	t.Helper()
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(hostKey)

	_, chans, reqs, err := ssh.NewServerConn(nc, config)
	if err != nil {
		t.Logf("mock SSH handshake: %v", err)
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			return
		}
		go handleMockSessionForRotate(ch, requests, newPubKeyB64)
	}
}

// handleMockSessionForRotate handles a single SSH session for the rotate sequence.
func handleMockSessionForRotate(ch ssh.Channel, reqs <-chan *ssh.Request, newPubKeyB64 string) {
	defer func() { _ = ch.Close() }()

	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		cmd := parseExecPayload(req.Payload)
		if req.WantReply {
			_ = req.Reply(true, nil)
		}

		// Drain client stdin before responding.
		_, _ = io.Copy(io.Discard, ch)

		switch {
		case strings.HasPrefix(cmd, "sudo grep"):
			_, _ = ch.Write([]byte("public=" + newPubKeyB64 + "\n"))
		case strings.HasPrefix(cmd, "sudo hop-agent setup --client-pubkey"):
			_, _ = ch.Write([]byte("ok\n"))
			// hop-agent rotate and systemctl restart need no output
		}

		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}
