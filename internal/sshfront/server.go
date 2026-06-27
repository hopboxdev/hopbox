package sshfront

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/core/box"
)

// Hub is the slice of the agent hub the front door bridges sessions through.
type Hub interface {
	Connected(workspaceID string) bool
	OpenShell(ctx context.Context, workspaceID string, hdr agentproto.ShellHeader) (io.ReadWriteCloser, error)
}

// Authority turns a client's public key into a principal (its identity). The
// default AnyKey authority accepts any key and uses its fingerprint — the
// krillbox "your key is your identity, no signup" model.
type Authority interface {
	Authenticate(key ssh.PublicKey) (principal string, err error)
}

// AnyKey accepts any public key; the principal is the key fingerprint.
type AnyKey struct{}

func (AnyKey) Authenticate(key ssh.PublicKey) (string, error) {
	return ssh.FingerprintSHA256(key), nil
}

// Server is the SSH front door. It terminates client SSH, maps username->spec
// and key->identity, ensures the box via the engine, and bridges the session in.
type Server struct {
	engine       *box.Engine
	hub          Hub
	hostKey      ssh.Signer
	authority    Authority
	images       func() []string // optional: catalog for the connect banner
	readyTimeout time.Duration
	pollInterval time.Duration
}

// WithImages makes the front door advertise the available images in its SSH
// connect banner, so users can discover what to put after `name:`.
func (s *Server) WithImages(list func() []string) *Server { s.images = list; return s }

// banner lists the available images (shown to the client before the session).
func (s *Server) banner() string {
	if s.images == nil {
		return ""
	}
	imgs := s.images()
	if len(imgs) == 0 {
		return ""
	}
	return fmt.Sprintf("\n  hopbox — compute boxes over SSH\n  images: %s\n  pick one:  ssh <name>:<image>@host   (omit :image for the default)\n\n",
		strings.Join(imgs, ", "))
}

// NewServer builds a front-door SSH server. authority defaults to AnyKey.
func NewServer(engine *box.Engine, hub Hub, hostKey ssh.Signer, authority Authority) *Server {
	if authority == nil {
		authority = AnyKey{}
	}
	return &Server{
		engine: engine, hub: hub, hostKey: hostKey, authority: authority,
		readyTimeout: 60 * time.Second,
		pollInterval: 200 * time.Millisecond,
	}
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		nc, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.handleConn(ctx, nc)
	}
}

// waitReady polls until the workspace's agent is connected or the timeout
// elapses — a freshly created box needs a moment to boot and dial back.
func (s *Server) waitReady(ctx context.Context, workspaceID string) error {
	deadline := time.NewTimer(s.readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(s.pollInterval)
	defer tick.Stop()
	if s.hub.Connected(workspaceID) {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("workspace %s not ready within %s", workspaceID, s.readyTimeout)
		case <-tick.C:
			if s.hub.Connected(workspaceID) {
				return nil
			}
		}
	}
}

// serveSession is the per-session loop: ensure the workspace named by username,
// wait for it to be ready, bridge the client byte stream to a shell in the box,
// and detach on exit so the reconciler can reap an ephemeral box.
func (s *Server) serveSession(ctx context.Context, principal, username string, hdr agentproto.ShellHeader, client io.ReadWriteCloser) error {
	b, release, err := s.engine.Attach(ctx, principal, username)
	if err != nil {
		return err
	}
	defer release()

	if err := s.waitReady(ctx, b.ID); err != nil {
		return err
	}
	shell, err := s.hub.OpenShell(ctx, b.ID, hdr)
	if err != nil {
		return fmt.Errorf("open shell: %w", err)
	}
	defer shell.Close()

	bridge(client, shell)
	return nil
}

// bridge copies between the client and the box shell. The session ends when the
// shell side closes — its output stream hits EOF, i.e. the box shell exited (or
// the client write fails because the client is gone). Client stdin reaching EOF
// (a one-shot `ssh host cmd`, which half-closes stdin immediately) must NOT end
// the session: it only half-closes the write side into the shell, so the shell's
// output keeps draining to the client until the command actually exits. Waiting
// on whichever side finished first — the old behaviour — truncated that output.
func bridge(client, shell io.ReadWriteCloser) {
	go func() {
		_, _ = io.Copy(shell, client) // client stdin -> shell
		// stdin done; tell the shell (half-close) without tearing the session down.
		if cw, ok := shell.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	_, _ = io.Copy(client, shell) // shell -> client; returns only when the shell exits
}

// --- SSH handshake glue (build-verified; exercised end-to-end, not in unit tests) ---

type ptyReq struct {
	Term          string
	Cols, Rows    uint32
	Width, Height uint32
	Modes         string
}

type execReq struct{ Command string }

func (s *Server) handleConn(ctx context.Context, nc net.Conn) {
	defer nc.Close()
	var principal string
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			p, err := s.authority.Authenticate(key)
			if err != nil {
				return nil, err
			}
			principal = p
			return &ssh.Permissions{}, nil
		},
		BannerCallback: func(ssh.ConnMetadata) string { return s.banner() },
	}
	cfg.AddHostKey(s.hostKey)

	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	username := conn.User()
	go ssh.DiscardRequests(reqs)

	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ctx, principal, username, ch, chReqs)
	}
}

func (s *Server) handleSession(ctx context.Context, principal, username string, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	hdr := agentproto.ShellHeader{}
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyReq
			if ssh.Unmarshal(req.Payload, &p) == nil {
				hdr.Cols, hdr.Rows = uint16(p.Cols), uint16(p.Rows)
			}
			_ = req.Reply(true, nil)
		case "window-change":
			_ = req.Reply(true, nil) // resize is best-effort; the shell is already attached
		case "shell", "exec":
			if req.Type == "exec" {
				var e execReq
				_ = ssh.Unmarshal(req.Payload, &e)
				hdr.Cmd = e.Command
			}
			_ = req.Reply(true, nil)
			err := s.serveSession(ctx, principal, username, hdr, ch)
			sendExit(ch, err)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// sendExit sends an exit-status so the client sees a clean close (0 on success).
func sendExit(ch ssh.Channel, err error) {
	code := uint32(0)
	if err != nil {
		code = 1
		_, _ = io.WriteString(ch, "hopbox: "+err.Error()+"\r\n")
	}
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, code)
	_, _ = ch.SendRequest("exit-status", false, payload)
}
