package boxstore_test

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/boxstore"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

type fakeCompute struct {
	destroyed int
	lastReq   ports.ProvisionRequest
}

func (f *fakeCompute) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	f.lastReq = r
	return ports.Instance{Ref: "c-1", Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Status(_ context.Context, ref string) (ports.Instance, error) {
	return ports.Instance{Ref: ref, Phase: ports.InstanceRunning}, nil
}
func (f *fakeCompute) Stop(context.Context, string) error    { return nil }
func (f *fakeCompute) Destroy(context.Context, string) error { f.destroyed++; return nil }

type fakeStorage struct{ ensured int }

func (f *fakeStorage) EnsureHome(_ context.Context, r ports.HomeRequest) (ports.Mount, error) {
	f.ensured++
	return ports.Mount{Source: "/data/" + r.WorkspaceID, Target: "/home/dev"}, nil
}
func (f *fakeStorage) Delete(context.Context, string) error { return nil }

type fakeIngress struct{ exposed, unexposed int }

func (f *fakeIngress) Expose(_ context.Context, r ports.ExposeRequest) (ports.Endpoint, error) {
	f.exposed++
	host := r.Name + "-" + r.WorkspaceID + ".gw"
	return ports.Endpoint{Ref: host, URL: "https://" + host, Name: r.Name, Port: r.Port}, nil
}
func (f *fakeIngress) Unexpose(context.Context, string) error { f.unexposed++; return nil }

func newStore(t *testing.T) store.Store {
	t.Helper()
	s, err := sqlite.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The dev-env hooks fold storage-home + ingress onto box.Reconciler: provision
// ensures a home (mount + env), running exposes ingress idempotently, destroy
// unexposes. This is the convergence replacing the old dev-env reconciler.
func TestHooksFoldStorageAndIngressOntoBoxReconciler(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp, strg, ig := &fakeCompute{}, &fakeStorage{}, &fakeIngress{}
	hooks := boxstore.NewHooks(st, strg, ig, boxstore.HooksConfig{AuthorizedKeys: "ssh-ed25519 AAA"})
	rec := box.NewReconciler(boxstore.New(st), comp, box.ReconcileConfig{AgentAddr: "h:7777", Hooks: hooks})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Ingress = []workspace.IngressPort{{Name: "app", Port: 3000}}
	if err := st.CreateWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}

	// provision: the storage home + dev-env env reach compute; HomeMount persists.
	if err := rec.ReconcileOne(ctx, "default", w.ID); err != nil {
		t.Fatal(err)
	}
	if strg.ensured != 1 {
		t.Fatalf("EnsureHome calls=%d want 1", strg.ensured)
	}
	if len(comp.lastReq.Mounts) != 1 || comp.lastReq.Mounts[0].Target != "/home/dev" {
		t.Fatalf("home mount not passed to compute: %+v", comp.lastReq.Mounts)
	}
	if comp.lastReq.Env["HOPBOX_AUTHORIZED_KEYS"] == "" || comp.lastReq.Env["HOPBOX_SSH_HOST_KEY"] == "" {
		t.Fatalf("dev-env env missing from provision: %+v", comp.lastReq.Env)
	}
	if got, _ := st.GetWorkspace(ctx, "default", w.ID); got.HomeMount == "" {
		t.Fatalf("HomeMount not persisted")
	}

	// running + connected: ingress exposed once (idempotent across ticks).
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	got.Phase, got.AgentConnected = box.PhaseRunning, true
	_ = st.UpdateWorkspace(ctx, got)
	for i := 0; i < 2; i++ {
		if err := rec.ReconcileOne(ctx, "default", w.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = st.GetWorkspace(ctx, "default", w.ID)
	if len(got.Endpoints) != 1 || got.Endpoints[0].URL != "https://app-"+w.ID+".gw" {
		t.Fatalf("endpoint not resolved: %+v", got.Endpoints)
	}
	if ig.exposed != 1 {
		t.Fatalf("expose=%d want 1 (idempotent)", ig.exposed)
	}

	// destroy: unexpose.
	got.Phase = box.PhaseDestroying
	_ = st.UpdateWorkspace(ctx, got)
	if err := rec.ReconcileOne(ctx, "default", w.ID); err != nil {
		t.Fatal(err)
	}
	if ig.unexposed != 1 {
		t.Fatalf("unexpose=%d want 1", ig.unexposed)
	}
}
