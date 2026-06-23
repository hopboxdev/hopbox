// Package agenthub is hopboxd's side of the agent reverse connection. It accepts
// agent dials on a TCP listener, authenticates the one-time bootstrap token,
// holds each workspace's yamux session, and opens pty shells on demand.
package agenthub

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/hashicorp/yamux"

	"github.com/hopboxdev/hopbox/internal/agentproto"
)

// TokenResolver maps a bootstrap token to its workspace id (the store provides this).
type TokenResolver func(ctx context.Context, token string) (workspaceID string, err error)

// StateSink lets the hub report connect/disconnect to the rest of the system.
type StateSink interface {
	SetAgentConnected(ctx context.Context, workspaceID string, connected bool)
}

type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*yamux.Session
	resolve  TokenResolver
	sink     StateSink
}

func New() *Hub {
	return &Hub{sessions: make(map[string]*yamux.Session)}
}

// WithResolver/WithSink configure the hub for live serving (the unit tests use
// New() + Register directly and need neither).
func (h *Hub) WithResolver(r TokenResolver) *Hub { h.resolve = r; return h }
func (h *Hub) WithSink(s StateSink) *Hub         { h.sink = s; return h }

func (h *Hub) Register(workspaceID string, sess *yamux.Session) {
	h.mu.Lock()
	if old := h.sessions[workspaceID]; old != nil {
		_ = old.Close()
	}
	h.sessions[workspaceID] = sess
	h.mu.Unlock()
	if h.sink != nil {
		h.sink.SetAgentConnected(context.Background(), workspaceID, true)
	}
}

func (h *Hub) Unregister(workspaceID string) {
	h.mu.Lock()
	delete(h.sessions, workspaceID)
	h.mu.Unlock()
	if h.sink != nil {
		h.sink.SetAgentConnected(context.Background(), workspaceID, false)
	}
}

func (h *Hub) Connected(workspaceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.sessions[workspaceID]
	return s != nil && !s.IsClosed()
}

func (h *Hub) get(workspaceID string) (*yamux.Session, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[workspaceID]
	return s, ok
}

// OpenShell opens a yamux stream to the workspace agent and writes the open +
// shell headers; the returned stream is then a raw bidirectional pty pipe.
func (h *Hub) OpenShell(ctx context.Context, workspaceID string, hdr agentproto.ShellHeader) (io.ReadWriteCloser, error) {
	stream, err := h.openStream(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := agentproto.WriteOpenFrame(stream, agentproto.OpenFrame{Kind: agentproto.KindShell}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write open frame: %w", err)
	}
	if err := agentproto.WriteShellHeader(stream, hdr); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write shell header: %w", err)
	}
	return stream, nil
}

// OpenForward opens a yamux stream and asks the agent to dial 127.0.0.1:port
// inside the workspace. The returned net.Conn is a raw pipe to that service —
// hopbox-gw uses it to proxy an inbound request into the workspace.
func (h *Hub) OpenForward(workspaceID string, port uint32) (net.Conn, error) {
	stream, err := h.openStream(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := agentproto.WriteOpenFrame(stream, agentproto.OpenFrame{Kind: agentproto.KindForward}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write open frame: %w", err)
	}
	if err := agentproto.WriteForwardHeader(stream, agentproto.ForwardHeader{Port: port}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write forward header: %w", err)
	}
	return stream, nil
}

// OpenSSH opens a yamux stream the agent will serve an SSH server on. The
// returned pipe carries the SSH wire protocol; the API bridges a client's
// `hopbox proxy` to it.
func (h *Hub) OpenSSH(workspaceID string) (io.ReadWriteCloser, error) {
	stream, err := h.openStream(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := agentproto.WriteOpenFrame(stream, agentproto.OpenFrame{Kind: agentproto.KindSSH}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write open frame: %w", err)
	}
	return stream, nil
}

// OpenExec opens a yamux stream and asks the agent to run cmd (argv, no pty).
// The returned stream yields exec frames (stdout/stderr/exit) via
// agentproto.ReadExecFrame.
func (h *Hub) OpenExec(workspaceID string, cmd []string) (io.ReadWriteCloser, error) {
	stream, err := h.openStream(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := agentproto.WriteOpenFrame(stream, agentproto.OpenFrame{Kind: agentproto.KindExec}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write open frame: %w", err)
	}
	if err := agentproto.WriteExecHeader(stream, agentproto.ExecHeader{Cmd: cmd}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("agenthub: write exec header: %w", err)
	}
	return stream, nil
}

func (h *Hub) openStream(workspaceID string) (*yamux.Stream, error) {
	sess, ok := h.get(workspaceID)
	if !ok || sess.IsClosed() {
		return nil, fmt.Errorf("agenthub: workspace %q has no connected agent", workspaceID)
	}
	stream, err := sess.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("agenthub: open stream: %w", err)
	}
	return stream, nil
}

// Serve accepts agent dials until ctx is cancelled or the listener closes.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error {
	if h.resolve == nil {
		return fmt.Errorf("agenthub: Serve requires a TokenResolver (call WithResolver)")
	}
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go h.accept(ctx, conn)
	}
}

func (h *Hub) accept(ctx context.Context, conn net.Conn) {
	hs, err := agentproto.ReadHandshake(conn)
	if err != nil {
		log.Printf("agenthub: handshake read: %v", err)
		_ = conn.Close()
		return
	}
	wsID, err := h.resolve(ctx, hs.Token)
	if err != nil || (hs.WorkspaceID != "" && hs.WorkspaceID != wsID) {
		log.Printf("agenthub: rejecting agent: bad token (ws=%q err=%v)", hs.WorkspaceID, err)
		_ = conn.Close()
		return
	}
	sess, err := yamux.Client(conn, nil)
	if err != nil {
		log.Printf("agenthub: yamux client: %v", err)
		_ = conn.Close()
		return
	}
	h.Register(wsID, sess)
	log.Printf("agenthub: agent connected for workspace %s", wsID)

	<-sess.CloseChan() // block until the agent session drops
	h.Unregister(wsID)
	log.Printf("agenthub: agent disconnected for workspace %s", wsID)
}
