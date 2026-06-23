package agenthub_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/agenthub"
	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/agentssh"
)

// TestOpenSSH_EndToEnd drives the whole novel path hermetically: a real agent
// dials the hub, the hub opens a KindSSH stream (as the API's SSH bridge would),
// the agent serves agentssh over it, and a real ssh client runs a command —
// proving `ssh <workspace>` works end to end without Docker.
func TestOpenSSH_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wsID, token = "ws1", "tok-123"
	hostKey := mustSigner(t)
	userSigner := mustSigner(t)

	hub := agenthub.New().WithResolver(func(_ context.Context, tok string) (string, error) {
		if tok == token {
			return wsID, nil
		}
		return "", context.Canceled
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go hub.Serve(ctx, ln)

	// fake in-process agent: dial in, handshake, serve yamux, dispatch KindSSH to
	// the real agentssh server (mirrors cmd/hopbox-agent).
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			return
		}
		if err := agentproto.WriteHandshake(conn, agentproto.Handshake{WorkspaceID: wsID, Token: token}); err != nil {
			return
		}
		sess, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		cfg := agentssh.Config{HostKey: hostKey, AuthorizedKeys: []ssh.PublicKey{userSigner.PublicKey()}, Shell: "/bin/sh"}
		for {
			stream, err := sess.Accept()
			if err != nil {
				return
			}
			go func(s net.Conn) {
				of, err := agentproto.ReadOpenFrame(s)
				if err != nil || of.Kind != agentproto.KindSSH {
					_ = s.Close()
					return
				}
				_ = agentssh.Serve(s, cfg)
			}(stream)
		}
	}()

	// wait for the agent to register
	deadline := time.Now().Add(5 * time.Second)
	for !hub.Connected(wsID) {
		if time.Now().After(deadline) {
			t.Fatal("agent never connected to hub")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// the API's SSH bridge does exactly this: open a KindSSH stream to the agent.
	rwc, err := hub.OpenSSH(wsID)
	if err != nil {
		t.Fatalf("OpenSSH: %v", err)
	}
	nc, ok := rwc.(net.Conn)
	if !ok {
		t.Fatal("OpenSSH stream is not a net.Conn")
	}

	cc, chans, reqs, err := ssh.NewClientConn(nc, "hopbox", &ssh.ClientConfig{
		User:            "dev",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(userSigner)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh client handshake: %v", err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	out, err := sess.Output("echo HOPBOX_SSH_E2E_OK")
	if err != nil {
		t.Fatalf("exec over ssh: %v", err)
	}
	if !strings.Contains(string(out), "HOPBOX_SSH_E2E_OK") {
		t.Fatalf("missing marker, got %q", out)
	}
}

func mustSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
