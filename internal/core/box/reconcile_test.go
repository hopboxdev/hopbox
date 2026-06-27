package box

import (
	"context"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

type fakeCompute struct {
	provisioned int
	destroyed   int
	status      ports.InstancePhase
}

func (f *fakeCompute) Provision(_ context.Context, _ ports.ProvisionRequest) (ports.Instance, error) {
	f.provisioned++
	return ports.Instance{Ref: "c-1", Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Status(_ context.Context, ref string) (ports.Instance, error) {
	return ports.Instance{Ref: ref, Phase: f.status}, nil
}
func (f *fakeCompute) Stop(context.Context, string) error { return nil }
func (f *fakeCompute) Destroy(context.Context, string) error {
	f.destroyed++
	return nil
}

func TestReconcileProvisionsPending(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	comp := &fakeCompute{}
	r := NewReconciler(st, comp, ReconcileConfig{AgentAddr: "host:7777"})
	b := New("default", "alice", "proj", "alpine")
	_ = st.Create(ctx, b)

	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseProvisioning {
		t.Fatalf("phase=%s want Provisioning", got.Phase)
	}
	if got.InstanceRef == "" || got.BootstrapToken == "" || comp.provisioned != 1 {
		t.Fatalf("provision incomplete: ref=%q token set=%v n=%d", got.InstanceRef, got.BootstrapToken != "", comp.provisioned)
	}
}

func TestReconcileReapsEphemeralOnDisconnect(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	comp := &fakeCompute{}
	r := NewReconciler(st, comp, ReconcileConfig{AgentAddr: "host:7777", Now: func() time.Time { return time.Now() }})

	b := New("default", "alice", "proj", "alpine")
	b.Phase = PhaseRunning
	b.InstanceRef = "c-1"
	b.Ephemeral = true // grace 0
	b.Attached = false
	b.AgentConnected = true
	_ = st.Create(ctx, b)

	// Running + ephemeral + detached + grace 0 -> Destroying.
	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseDestroying {
		t.Fatalf("phase=%s want Destroying", got.Phase)
	}
	// Next tick: destroy the instance and delete the box.
	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	if comp.destroyed != 1 {
		t.Fatalf("compute.Destroy called %d times, want 1", comp.destroyed)
	}
	if _, err := st.Get(ctx, "default", b.ID); err != ErrNotFound {
		t.Fatal("reaped box must be deleted from the store")
	}
}

type fakeSuspender struct {
	*fakeCompute
	suspended, resumed int
}

func (f *fakeSuspender) Suspend(context.Context, string) error { f.suspended++; return nil }
func (f *fakeSuspender) Resume(context.Context, string) error  { f.resumed++; return nil }

func idleRunningBox(now time.Time) *Box {
	b := New("default", "alice", "proj", "alpine")
	b.Phase = PhaseRunning
	b.InstanceRef = "c-1"
	b.AutoSuspend = true
	b.AgentConnected = true
	b.Attached = false
	b.Load = 0.0
	b.LastActive = now.Add(-10 * time.Minute) // idle for 10m
	return b
}

func TestReconcileAutoSuspendsThenResumes(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	comp := &fakeSuspender{fakeCompute: &fakeCompute{}}
	now := time.Now()
	r := NewReconciler(st, comp, ReconcileConfig{
		AgentAddr: "host:7777",
		Idle:      IdleConfig{Timeout: 5 * time.Minute, LoadThreshold: 0.2},
		Now:       func() time.Time { return now },
	})
	b := idleRunningBox(now)
	_ = st.Create(ctx, b)

	// idle persistent AutoSuspend box -> Suspended (not reaped).
	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseSuspended || comp.suspended != 1 || got.AgentConnected {
		t.Fatalf("want suspended: phase=%s n=%d agent=%v", got.Phase, comp.suspended, got.AgentConnected)
	}

	// suspend != dead: a suspended box must not be re-provisioned.
	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseSuspended || comp.provisioned != 0 {
		t.Fatalf("suspended box must stay put: phase=%s prov=%d", got.Phase, comp.provisioned)
	}

	// attach -> resume.
	got.Attached = true
	_ = st.Update(ctx, got)
	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseRunning || comp.resumed != 1 {
		t.Fatalf("attach should resume: phase=%s resumed=%d", got.Phase, comp.resumed)
	}
}

func TestReconcileKeepAlivePinPreventsSuspend(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	comp := &fakeSuspender{fakeCompute: &fakeCompute{}}
	now := time.Now()
	r := NewReconciler(st, comp, ReconcileConfig{
		Idle: IdleConfig{Timeout: 5 * time.Minute, LoadThreshold: 0.2},
		Now:  func() time.Time { return now },
	})
	b := idleRunningBox(now)
	b.KeepAliveUntil = now.Add(time.Hour) // pinned alive
	_ = st.Create(ctx, b)

	if err := r.ReconcileOne(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, "default", b.ID)
	if got.Phase != PhaseRunning || comp.suspended != 0 {
		t.Fatalf("keep-alive must prevent suspend: phase=%s n=%d", got.Phase, comp.suspended)
	}
}
