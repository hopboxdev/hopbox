package sshfront

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// reuse a minimal in-memory store (white-box: same package as Manager).
type memStore struct {
	byName map[string]*workspace.Workspace
}

func newMemStore() *memStore { return &memStore{byName: map[string]*workspace.Workspace{}} }
func (s *memStore) GetByName(_ context.Context, t, n string) (*workspace.Workspace, error) {
	if w, ok := s.byName[t+"/"+n]; ok {
		return w, nil
	}
	return nil, store.ErrNotFound
}
func (s *memStore) CreateWorkspace(_ context.Context, w *workspace.Workspace) error {
	s.byName[w.TenantID+"/"+w.Name] = w
	return nil
}
func (s *memStore) UpdateWorkspace(_ context.Context, w *workspace.Workspace) error {
	s.byName[w.TenantID+"/"+w.Name] = w
	return nil
}

type fakeHub struct {
	connectAfter int
	checks       int
	lastHdr      agentproto.ShellHeader
	opened       bool
}

func (h *fakeHub) Connected(string) bool { h.checks++; return h.checks >= h.connectAfter }
func (h *fakeHub) OpenShell(_ context.Context, _ string, hdr agentproto.ShellHeader) (io.ReadWriteCloser, error) {
	h.lastHdr, h.opened = hdr, true
	return eofShell{}, nil
}

// eofShell ends the bridge immediately: Read EOFs, Write is discarded.
type eofShell struct{}

func (eofShell) Read([]byte) (int, error)    { return 0, io.EOF }
func (eofShell) Write(p []byte) (int, error) { return len(p), nil }
func (eofShell) Close() error                { return nil }

func newServer(st Store, hub Hub) *Server {
	m := New(st, nil, Config{Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"}})
	return &Server{
		mgr:          m,
		hub:          hub,
		readyTimeout: time.Second,
		pollInterval: time.Millisecond,
	}
}

func TestServeSessionBridgesAndDetaches(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	hub := &fakeHub{connectAfter: 1}
	s := newServer(st, hub)

	hdr := agentproto.ShellHeader{Cols: 80, Rows: 24}
	if err := s.serveSession(ctx, "alice", "proj:python", hdr, eofShell{}); err != nil {
		t.Fatalf("serveSession: %v", err)
	}
	if !hub.opened {
		t.Fatal("serveSession must open a shell in the box")
	}
	if hub.lastHdr.Cols != 80 || hub.lastHdr.Rows != 24 {
		t.Fatalf("pty header not forwarded: %+v", hub.lastHdr)
	}
	got, _ := st.GetByName(ctx, "default", "proj")
	if got == nil || got.Attached {
		t.Fatal("session end must detach the workspace (release)")
	}
}

// lateClient models a one-shot `ssh host cmd` client: it sends no stdin (Read
// EOFs at once) and captures everything written back to it.
type lateClient struct{ out bytes.Buffer }

func (c *lateClient) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *lateClient) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *lateClient) Close() error                { return nil }

// lateShell emits its output only after a beat — a command that produces output
// after the client has already half-closed stdin — then EOFs. It records the
// half-close so the test can assert stdin EOF was propagated, not swallowed.
type lateShell struct {
	out      []byte
	emitted  bool
	writeEnd bool
}

func (s *lateShell) Read(p []byte) (int, error) {
	if !s.emitted {
		time.Sleep(20 * time.Millisecond)
		s.emitted = true
		return copy(p, s.out), nil
	}
	return 0, io.EOF
}
func (s *lateShell) Write(p []byte) (int, error) { return len(p), nil }
func (s *lateShell) Close() error                { return nil }
func (s *lateShell) CloseWrite() error           { s.writeEnd = true; return nil }

// TestBridgeDrainsShellOutputAfterStdinEOF guards the exec race: a non-interactive
// client half-closes stdin immediately, but the shell's output must still drain
// in full before the bridge returns. The old "return on whichever copy finished
// first" behaviour truncated it.
func TestBridgeDrainsShellOutputAfterStdinEOF(t *testing.T) {
	client := &lateClient{}
	shell := &lateShell{out: []byte("HOPBOX_BRIDGE_OK")}

	done := make(chan struct{})
	go func() { bridge(client, shell); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not return")
	}

	if got := client.out.String(); !strings.Contains(got, "HOPBOX_BRIDGE_OK") {
		t.Fatalf("bridge truncated shell output on stdin EOF: %q", got)
	}
	if !shell.writeEnd {
		t.Fatal("bridge must half-close the shell write side when client stdin EOFs")
	}
}

func TestWaitReadyTimesOut(t *testing.T) {
	st := newMemStore()
	hub := &fakeHub{connectAfter: 1 << 30} // never connects
	s := newServer(st, hub)
	s.readyTimeout = 20 * time.Millisecond

	if err := s.waitReady(context.Background(), "w1"); err == nil {
		t.Fatal("waitReady must error when the agent never connects")
	}
}

func TestWaitReadySucceedsAfterPolls(t *testing.T) {
	st := newMemStore()
	hub := &fakeHub{connectAfter: 3}
	s := newServer(st, hub)
	if err := s.waitReady(context.Background(), "w1"); err != nil {
		t.Fatalf("waitReady should succeed once connected: %v", err)
	}
}
