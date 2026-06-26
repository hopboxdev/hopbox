package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	db := filepath.Join(t.TempDir(), "test.db")
	s, err := sqlite.Open(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.BootstrapToken = "tok123"
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetWorkspace(ctx, "default", w.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "proj" || got.ImageRef != "ubuntu:24.04" || got.Phase != box.PhasePending {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestBackendAndLifetimeRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Backend = "docker"
	w.Ephemeral = true
	w.Grace = 5 * time.Minute
	w.MaxTTL = time.Hour
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	// reconciler stamps a deadline on detach
	d := w.CreatedAt.Add(5 * time.Minute)
	w.Deadline = &d
	if err := s.UpdateWorkspace(ctx, w); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetWorkspace(ctx, "default", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "docker" {
		t.Fatalf("backend=%q want docker", got.Backend)
	}
	if !got.Ephemeral || got.Grace != 5*time.Minute || got.MaxTTL != time.Hour {
		t.Fatalf("lifetime round-trip: ephemeral=%v grace=%v maxttl=%v", got.Ephemeral, got.Grace, got.MaxTTL)
	}
	if got.Deadline == nil || !got.Deadline.Equal(d) {
		t.Fatalf("deadline round-trip: got %v want %v", got.Deadline, d)
	}
}

func TestIngressAndEndpointsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Ingress = []workspace.IngressPort{{Name: "app", Port: 3000}}
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	// reconciler later resolves and persists an endpoint
	w.Endpoints = []workspace.Endpoint{{Name: "app", URL: "https://app-x.gw", Port: 3000, Ref: "app-x.gw"}}
	if err := s.UpdateWorkspace(ctx, w); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetWorkspace(ctx, "default", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ingress) != 1 || got.Ingress[0] != (workspace.IngressPort{Name: "app", Port: 3000}) {
		t.Fatalf("ingress spec round-trip: %+v", got.Ingress)
	}
	if len(got.Endpoints) != 1 || got.Endpoints[0].URL != "https://app-x.gw" || got.Endpoints[0].Ref != "app-x.gw" {
		t.Fatalf("endpoints round-trip: %+v", got.Endpoints)
	}
}

func TestGetByNameAndToken(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.BootstrapToken = "tok-abc"
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	byName, err := s.GetByName(ctx, "default", "proj")
	if err != nil || byName.ID != w.ID {
		t.Fatalf("byname: %+v err=%v", byName, err)
	}
	byTok, err := s.GetByToken(ctx, "tok-abc")
	if err != nil || byTok.ID != w.ID {
		t.Fatalf("bytoken: %+v err=%v", byTok, err)
	}
}

func TestUpdateAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	if err := s.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	w.Phase = box.PhaseRunning
	w.AgentConnected = true
	w.InstanceRef = "c-1"
	if err := s.UpdateWorkspace(ctx, w); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseRunning || !got.AgentConnected || got.InstanceRef != "c-1" {
		t.Fatalf("update not persisted: %+v", got)
	}
	all, err := s.ListAll(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("listall: %d err=%v", len(all), err)
	}
}

func TestNotFoundAndDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.GetWorkspace(ctx, "default", "nope"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	_ = s.CreateWorkspace(ctx, w)
	if err := s.DeleteWorkspace(ctx, "default", w.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetWorkspace(ctx, "default", w.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound after delete got %v", err)
	}
}
