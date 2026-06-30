package sshfront

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

// memStore is a minimal in-memory box.Store for the front-door server tests.
type memStore struct{ byKey map[string]*box.Box }

func newMemStore() *memStore { return &memStore{byKey: map[string]*box.Box{}} }
func (s *memStore) put(b *box.Box) {
	s.byKey["n/"+b.TenantID+"/"+b.Name] = b
	s.byKey["i/"+b.TenantID+"/"+b.ID] = b
}
func (s *memStore) Get(_ context.Context, t, id string) (*box.Box, error) {
	if b, ok := s.byKey["i/"+t+"/"+id]; ok {
		return b, nil
	}
	return nil, box.ErrNotFound
}
func (s *memStore) GetByName(_ context.Context, t, n string) (*box.Box, error) {
	if b, ok := s.byKey["n/"+t+"/"+n]; ok {
		return b, nil
	}
	return nil, box.ErrNotFound
}
func (s *memStore) List(context.Context, string) ([]*box.Box, error) { return nil, nil }
func (s *memStore) Create(_ context.Context, b *box.Box) error       { s.put(b); return nil }
func (s *memStore) Update(_ context.Context, b *box.Box) error       { s.put(b); return nil }
func (s *memStore) Delete(context.Context, string, string) error     { return nil }

// fakeHub drives waitReady: Connected flips true after connectAfter checks.
// (The SSH proxy itself is exercised end-to-end on a live box, not here.)
type fakeHub struct {
	connectAfter int
	checks       int
}

func (h *fakeHub) Connected(string) bool { h.checks++; return h.checks >= h.connectAfter }
func (h *fakeHub) OpenSSH(string) (io.ReadWriteCloser, error) {
	return nil, io.ErrClosedPipe // not reached by these tests
}

func newServer(st box.Store, hub Hub) *Server {
	e := box.NewEngine(st, nil, box.EngineConfig{Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"}})
	return &Server{
		engine:       e,
		hub:          hub,
		readyTimeout: time.Second,
		pollInterval: time.Millisecond,
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
