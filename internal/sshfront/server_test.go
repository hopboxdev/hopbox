package sshfront

import (
	"context"
	"io"
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
