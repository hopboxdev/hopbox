package sshfront

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"slices"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

// Hub is the slice of the agent hub the front door bridges sessions through.
type Hub interface {
	Connected(workspaceID string) bool
	// OpenSSH returns a stream the in-box agent serves an SSH server on; the front
	// door proxies the authenticated client's session into it.
	OpenSSH(workspaceID string) (io.ReadWriteCloser, error)
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
	images       func() []string // optional: catalog for the `images` meta-command
	readyTimeout time.Duration
	pollInterval time.Duration
}

// WithImages lets users discover the catalog: `ssh images@host` lists the
// available image names (and spawns no box).
func (s *Server) WithImages(list func() []string) *Server { s.images = list; return s }

// writeImages prints the catalog to the client. Returns false if image listing
// isn't available (so the caller falls back to spawning a box of that name).
func (s *Server) writeImages(w io.Writer) bool {
	if s.images == nil {
		return false
	}
	imgs := s.images()
	if len(imgs) == 0 {
		return false
	}
	fmt.Fprint(w, "available images (use  ssh <name>:<image>@host):\r\n")
	for _, img := range imgs {
		fmt.Fprintf(w, "  %s\r\n", img)
	}
	return true
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
	ready := func() (bool, error) {
		if s.hub.Connected(workspaceID) {
			return true, nil
		}
		// Abort early if the box failed to start, instead of waiting out the whole
		// timeout for an agent that will never connect.
		if ph, msg, ok := s.engine.State(ctx, workspaceID); ok && ph == box.PhaseFailed {
			return false, fmt.Errorf("box failed to start: %s", msg)
		}
		return false, nil
	}
	if ok, err := ready(); ok || err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("workspace %s not ready within %s", workspaceID, s.readyTimeout)
		case <-tick.C:
			if ok, err := ready(); ok || err != nil {
				return err
			}
		}
	}
}

// route resolves the box the username names, then proxies the client's SSH
// session into the box's own SSH server (agentssh) — which handles interactive
// shells, exec (pty and raw), and the sftp subsystem, so scp/sftp/rsync and clean
// `ssh host cmd` all behave like a normal SSH host. The front door has already
// authenticated the user; the box trusts the proxied session (TrustedSSH).
func (s *Server) route(ctx context.Context, principal, username string, chans <-chan ssh.NewChannel) {
	spec, perr := box.ParseSpec(username)
	// `ssh images@host` is a meta-command: list the catalog, spawn no box.
	if perr == nil && (spec.Name == "images" || spec.Name == "image") && s.images != nil && len(s.images()) > 0 {
		s.replyAll(chans, func(w io.Writer) { s.writeImages(w) }, 0)
		return
	}
	// Fail fast on an unknown image rather than booting a box that can't provision.
	if perr == nil && spec.Image != "" && s.images != nil {
		if cat := s.images(); len(cat) > 0 && !slices.Contains(cat, spec.Image) {
			s.replyAll(chans, msg("unknown image %q — run  ssh images@host  to list the catalog", spec.Image), 1)
			return
		}
	}
	b, release, err := s.engine.Attach(ctx, principal, username)
	if err != nil {
		s.replyAll(chans, msg("hopbox: %v", err), 1)
		return
	}
	defer release()
	if err := s.waitReady(ctx, b.ID); err != nil {
		s.replyAll(chans, msg("hopbox: %v", err), 1)
		return
	}
	up, err := s.dialAgentSSH(b.ID)
	if err != nil {
		s.replyAll(chans, msg("hopbox: connect to box: %v", err), 1)
		return
	}
	defer up.Close()
	for nch := range chans {
		go s.proxyChannel(nch, up)
	}
}

// dialAgentSSH opens an SSH client to the box's agentssh over the control-plane
// stream. No client auth: the stream is reachable only by the front door, which
// already authenticated the user, and the box accepts it (TrustedSSH).
func (s *Server) dialAgentSSH(workspaceID string) (ssh.Conn, error) {
	stream, err := s.hub.OpenSSH(workspaceID)
	if err != nil {
		return nil, err
	}
	nc, ok := stream.(net.Conn)
	if !ok {
		nc = rwcConn{stream}
	}
	cc, chans, reqs, err := ssh.NewClientConn(nc, "box", &ssh.ClientConfig{
		User:            "box",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // control-plane stream; no host key to pin
		Timeout:         15 * time.Second,
	})
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "no reverse channels")
		}
	}()
	return cc, nil
}

// proxyChannel pipes one client session channel to a fresh session channel on the
// box's SSH server, forwarding requests (pty-req, env, shell/exec/subsystem,
// window-change, exit-status) and data both ways — a generic SSH session relay.
func (s *Server) proxyChannel(nch ssh.NewChannel, up ssh.Conn) {
	if nch.ChannelType() != "session" {
		_ = nch.Reject(ssh.UnknownChannelType, "only session channels are supported")
		return
	}
	upCh, upReqs, err := up.OpenChannel("session", nil)
	if err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, "box session: "+err.Error())
		return
	}
	dnCh, dnReqs, err := nch.Accept()
	if err != nil {
		_ = upCh.Close()
		return
	}
	go forwardRequests(dnReqs, upCh) // client -> box
	go func() { _, _ = io.Copy(upCh, dnCh); _ = upCh.CloseWrite() }()
	go func() { _, _ = io.Copy(dnCh.Stderr(), upCh.Stderr()) }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(dnCh, upCh); close(done) }()
	forwardRequests(upReqs, dnCh) // box -> client (exit-status); returns when the box closes the channel
	<-done
	_ = dnCh.Close()
	_ = upCh.Close()
}

func forwardRequests(in <-chan *ssh.Request, out ssh.Channel) {
	for r := range in {
		ok, _ := out.SendRequest(r.Type, r.WantReply, r.Payload)
		if r.WantReply {
			_ = r.Reply(ok, nil)
		}
	}
}

// replyAll answers each session channel with a one-shot message + exit code (for
// the `images` meta-command and pre-box errors), spawning no box.
func (s *Server) replyAll(chans <-chan ssh.NewChannel, write func(io.Writer), code uint32) {
	for nch := range chans {
		go replySession(nch, write, code)
	}
}

func replySession(nch ssh.NewChannel, write func(io.Writer), code uint32) {
	if nch.ChannelType() != "session" {
		_ = nch.Reject(ssh.UnknownChannelType, "only session channels are supported")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "shell", "exec", "subsystem":
			_ = req.Reply(true, nil)
			if write != nil {
				write(ch)
			}
			sendExitCode(ch, code)
			return
		default:
			_ = req.Reply(req.Type == "pty-req" || req.Type == "env" || req.Type == "window-change", nil)
		}
	}
}

func msg(format string, a ...any) func(io.Writer) {
	return func(w io.Writer) { fmt.Fprintf(w, format+"\r\n", a...) }
}

// rwcConn adapts a stream to net.Conn for ssh.NewClientConn (yamux streams
// already satisfy net.Conn; this is the fallback).
type rwcConn struct{ io.ReadWriteCloser }

func (rwcConn) LocalAddr() net.Addr                { return boxAddr{} }
func (rwcConn) RemoteAddr() net.Addr               { return boxAddr{} }
func (rwcConn) SetDeadline(_ time.Time) error      { return nil }
func (rwcConn) SetReadDeadline(_ time.Time) error  { return nil }
func (rwcConn) SetWriteDeadline(_ time.Time) error { return nil }

type boxAddr struct{}

func (boxAddr) Network() string { return "yamux" }
func (boxAddr) String() string  { return "box" }

// --- SSH handshake glue (build-verified; exercised end-to-end, not in unit tests) ---

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
	}
	cfg.AddHostKey(s.hostKey)

	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	username := conn.User()
	go ssh.DiscardRequests(reqs)
	s.route(ctx, principal, username, chans)
}

// sendExitCode sends an exit-status so the client sees a clean close.
func sendExitCode(ch ssh.Channel, code uint32) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, code)
	_, _ = ch.SendRequest("exit-status", false, payload)
}
