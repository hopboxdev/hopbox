package reconciler_test

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/core/reconciler"
	"github.com/mesadev/mesa/internal/core/store"
	"github.com/mesadev/mesa/internal/core/store/sqlite"
	"github.com/mesadev/mesa/internal/core/workspace"
)

// --- fakes ---

type fakeCompute struct {
	provisioned int
	destroyed   int
	phase       ports.InstancePhase
}

func (f *fakeCompute) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	f.provisioned++
	return ports.Instance{Ref: "c-" + r.WorkspaceID, Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Status(_ context.Context, ref string) (ports.Instance, error) {
	ph := f.phase
	if ph == "" {
		ph = ports.InstanceRunning
	}
	return ports.Instance{Ref: ref, Phase: ph}, nil
}
func (f *fakeCompute) Stop(context.Context, string) error { return nil }
func (f *fakeCompute) Destroy(_ context.Context, _ string) error {
	f.destroyed++
	return nil
}

type fakeStorage struct{ ensured int }

func (f *fakeStorage) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	f.ensured++
	return ports.Mount{Source: "/data/" + r.WorkspaceID, Target: "/home/dev"}, nil
}
func (f *fakeStorage) Delete(_ context.Context, _ string) error { return nil }

func newStore(t *testing.T) store.Store {
	t.Helper()
	s, err := sqlite.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPendingProvisions(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp, strg := &fakeCompute{}, &fakeStorage{}
	r := reconciler.New(st, comp, strg, reconciler.Config{AgentAddr: "host:7777", AgentPath: "/x/agent"})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != workspace.PhaseProvisioning {
		t.Fatalf("phase=%s want Provisioning", got.Phase)
	}
	if comp.provisioned != 1 || strg.ensured != 1 {
		t.Fatalf("provisioned=%d ensured=%d", comp.provisioned, strg.ensured)
	}
	if got.InstanceRef == "" || got.HomeMount == "" || got.BootstrapToken == "" {
		t.Fatalf("status not populated: %+v", got)
	}
}

func TestProvisioningBecomesRunningWhenAgentConnects(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = workspace.PhaseProvisioning
	w.InstanceRef = "c-x"
	w.AgentConnected = true // agenthub flipped this
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != workspace.PhaseRunning {
		t.Fatalf("phase=%s want Running", got.Phase)
	}
}

func TestRunningWithDeadAgentReprovisions(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{phase: ports.InstanceGone}
	r := reconciler.New(st, comp, &fakeStorage{}, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = workspace.PhaseRunning
	w.InstanceRef = "c-dead"
	w.AgentConnected = false // agent dropped
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != workspace.PhaseProvisioning {
		t.Fatalf("phase=%s want Provisioning (self-heal)", got.Phase)
	}
	if comp.provisioned != 1 {
		t.Fatalf("expected re-provision, got %d", comp.provisioned)
	}
}

func TestDestroyingRemoves(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{}
	r := reconciler.New(st, comp, &fakeStorage{}, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = workspace.PhaseDestroying
	w.InstanceRef = "c-1"
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	if comp.destroyed != 1 {
		t.Fatalf("destroyed=%d want 1", comp.destroyed)
	}
	if _, err := st.GetWorkspace(ctx, "default", w.ID); err != store.ErrNotFound {
		t.Fatalf("workspace should be gone, err=%v", err)
	}
}
