package reconciler_test

import (
	"context"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

var rt0 = time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

func clockAt(t *time.Time) func() time.Time { return func() time.Time { return *t } }

func TestEphemeralDisconnectReaps(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{}
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{Now: func() time.Time { return rt0 }})

	w := workspace.New("default", "alice", "proj", "img")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-eph"
	w.Ephemeral = true // grace 0: reap on disconnect
	w.Attached = false
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseDestroying {
		t.Fatalf("phase=%s want Destroying", got.Phase)
	}
	// must NOT self-heal an ephemeral box like a persistent one.
	if comp.provisioned != 0 {
		t.Fatalf("ephemeral disconnect must not re-provision, got %d", comp.provisioned)
	}
}

func TestEphemeralGraceStampsThenReaps(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	now := rt0
	comp := &fakeCompute{}
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{Now: clockAt(&now)})

	w := workspace.New("default", "alice", "proj", "img")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-eph"
	w.Ephemeral = true
	w.Grace = 5 * time.Minute
	w.Attached = false
	_ = st.CreateWorkspace(ctx, w)

	// first tick: stamp deadline, stay Running.
	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseRunning {
		t.Fatalf("phase=%s want Running (within grace)", got.Phase)
	}
	if got.Deadline == nil || !got.Deadline.Equal(rt0.Add(5*time.Minute)) {
		t.Fatalf("deadline=%v want %v", got.Deadline, rt0.Add(5*time.Minute))
	}

	// advance past the deadline: reap.
	now = rt0.Add(6 * time.Minute)
	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseDestroying {
		t.Fatalf("phase=%s want Destroying (past deadline)", got.Phase)
	}
}

func TestEphemeralReconnectClearsDeadline(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, nil, reconciler.Config{Now: func() time.Time { return rt0 }})

	w := workspace.New("default", "alice", "proj", "img")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-eph"
	w.Ephemeral = true
	w.Grace = 5 * time.Minute
	d := rt0.Add(5 * time.Minute)
	w.Deadline = &d
	w.Attached = true
	w.AgentConnected = true // reconnected within grace
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseRunning {
		t.Fatalf("phase=%s want Running", got.Phase)
	}
	if got.Deadline != nil {
		t.Fatalf("deadline should be cleared on reconnect, got %v", got.Deadline)
	}
}
