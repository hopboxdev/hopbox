package setup_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/setup"
)

// TestBootstrapKeyExchange drives the full Bootstrap() sequence against an
// in-process mock SSH server. It verifies that:
//   - Bootstrap returns without error
//   - The returned HostConfig has the expected name, host, and port
//   - The TOFU SSH host key is captured and stored
//   - The server's WireGuard public key (returned by the mock) is recorded
func TestBootstrapKeyExchange(t *testing.T) {
	// Generate a random 32-byte WireGuard public key for the mock to return.
	rawPub := make([]byte, 32)
	if _, err := rand.Read(rawPub); err != nil {
		t.Fatalf("rand: %v", err)
	}
	serverPubKeyB64 := base64.StdEncoding.EncodeToString(rawPub)

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
		serveMockSSH(t, conn, hostKey, serverPubKeyB64)
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	// Write a client SSH private key to a temp file.
	keyFile := writeClientSSHKey(t)

	// Point HOP_AGENT_BINARY at a small readable file (any content will do).
	agentBin := filepath.Join(t.TempDir(), "hop-agent")
	if err := os.WriteFile(agentBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Redirect HOME so cfg.Save() writes under a temp directory.
	t.Setenv("HOME", t.TempDir())
	// Clear SSH_AUTH_SOCK so LoadSigners doesn't consult the host's SSH agent.
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOP_AGENT_BINARY", agentBin)

	opts := setup.Options{
		Name:          "testbox",
		SSHHost:       "127.0.0.1",
		SSHPort:       port,
		SSHUser:       "testuser",
		SSHKeyPath:    keyFile,
		ConfirmReader: strings.NewReader("yes\n"),
	}

	cfg, err := setup.Bootstrap(context.Background(), opts, io.Discard)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if cfg.Name != "testbox" {
		t.Errorf("cfg.Name = %q, want %q", cfg.Name, "testbox")
	}
	if cfg.SSHHost != "127.0.0.1" {
		t.Errorf("cfg.SSHHost = %q, want %q", cfg.SSHHost, "127.0.0.1")
	}
	if cfg.SSHPort != port {
		t.Errorf("cfg.SSHPort = %d, want %d", cfg.SSHPort, port)
	}
	if cfg.SSHHostKey == "" {
		t.Error("cfg.SSHHostKey is empty; TOFU host key was not captured")
	}
	if cfg.PeerPublicKey != serverPubKeyB64 {
		t.Errorf("cfg.PeerPublicKey = %q, want %q", cfg.PeerPublicKey, serverPubKeyB64)
	}
}

// writeClientSSHKey generates an ed25519 SSH private key and writes it in
// OpenSSH PEM format to a temp file. Returns the file path.
func writeClientSSHKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return keyFile
}

// serveMockSSH handles a single SSH connection, dispatching each session
// channel to handleMockSession. pubKeyB64 is the WireGuard public key
// returned in response to the grep command during key exchange.
func serveMockSSH(t *testing.T, nc net.Conn, hostKey ssh.Signer, pubKeyB64 string) {
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
		go handleMockSession(ch, requests, pubKeyB64)
	}
}

// handleMockSession handles a single SSH session channel. It drains stdin
// (blocking until the client closes it) before sending the exit-status, so
// SCP file uploads complete correctly even for large payloads.
func handleMockSession(ch ssh.Channel, reqs <-chan *ssh.Request, pubKeyB64 string) {
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

		// Drain client stdin before responding. For exec sessions without stdin
		// the client sends EOF immediately; for SCP (tee) sessions this reads
		// the uploaded file data before we close the channel.
		_, _ = io.Copy(io.Discard, ch)

		// The grep command is how Bootstrap reads the server's WireGuard pubkey.
		if strings.HasPrefix(cmd, "sudo grep") {
			_, _ = ch.Write([]byte("public=" + pubKeyB64 + "\n"))
		}

		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

func parseExecPayload(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	length := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if len(payload) < 4+length {
		return ""
	}
	return string(payload[4 : 4+length])
}

func generateEd25519Key() (ssh.Signer, error) {
	privateKey, err := generateEd25519KeyRaw()
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(privateKey)
}
