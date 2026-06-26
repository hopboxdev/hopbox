package reconciler_test

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/store/sqlite"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// --- fakes ---

type fakeCompute struct {
	provisioned int
	destroyed   int
	phase       ports.InstancePhase
	lastEnv     map[string]string
}

func (f *fakeCompute) Provision(_ context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	f.provisioned++
	f.lastEnv = r.Env
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

type fakeIngress struct {
	exposed   int
	unexposed int
}

func (f *fakeIngress) Expose(_ context.Context, r ports.ExposeRequest) (ports.Endpoint, error) {
	f.exposed++
	host := r.Name + "-" + r.WorkspaceID + ".gw"
	return ports.Endpoint{Ref: host, URL: "https://" + host, Name: r.Name, Port: r.Port}, nil
}
func (f *fakeIngress) Unexpose(_ context.Context, _ string) error { f.unexposed++; return nil }

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
	r := reconciler.New(st, comp, strg, nil, reconciler.Config{AgentAddr: "host:7777", Agent: ports.AgentImage{HostBinaryPath: "/x/agent"}})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseProvisioning {
		t.Fatalf("phase=%s want Provisioning", got.Phase)
	}
	if comp.provisioned != 1 || strg.ensured != 1 {
		t.Fatalf("provisioned=%d ensured=%d", comp.provisioned, strg.ensured)
	}
	if got.InstanceRef == "" || got.HomeMount == "" || got.BootstrapToken == "" {
		t.Fatalf("status not populated: %+v", got)
	}
}

func TestProvisionInjectsSSHEnv(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp, strg := &fakeCompute{}, &fakeStorage{}
	r := reconciler.New(st, comp, strg, nil, reconciler.Config{
		AgentAddr:      "host:7777",
		Agent:          ports.AgentImage{HostBinaryPath: "/x/agent"},
		AuthorizedKeys: "ssh-ed25519 AAAAkey user@host\n",
	})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	_ = st.CreateWorkspace(ctx, w)
	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if got := comp.lastEnv["HOPBOX_AUTHORIZED_KEYS"]; got != "ssh-ed25519 AAAAkey user@host\n" {
		t.Fatalf("HOPBOX_AUTHORIZED_KEYS = %q", got)
	}
	// host key persists on the home volume (/home/dev) so known_hosts survives restarts.
	if got := comp.lastEnv["HOPBOX_SSH_HOST_KEY"]; got != "/home/dev/.hopbox/ssh_host_ed25519_key" {
		t.Fatalf("HOPBOX_SSH_HOST_KEY = %q", got)
	}
}

// With no authorized keys configured, SSH stays off: the key var is absent.
func TestProvisionNoSSHWhenUnconfigured(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{}
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{AgentAddr: "host:7777"})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	_ = st.CreateWorkspace(ctx, w)
	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := comp.lastEnv["HOPBOX_AUTHORIZED_KEYS"]; ok {
		t.Fatal("HOPBOX_AUTHORIZED_KEYS should be absent when no keys configured")
	}
}

func TestProvisioningBecomesRunningWhenAgentConnects(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, nil, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseProvisioning
	w.InstanceRef = "c-x"
	w.AgentConnected = true // agenthub flipped this
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseRunning {
		t.Fatalf("phase=%s want Running", got.Phase)
	}
}

func TestRunningWithDeadAgentReprovisions(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{phase: ports.InstanceGone}
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-dead"
	w.AgentConnected = false // agent dropped
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseProvisioning {
		t.Fatalf("phase=%s want Provisioning (self-heal)", got.Phase)
	}
	if comp.provisioned != 1 {
		t.Fatalf("expected re-provision, got %d", comp.provisioned)
	}
}

func TestRunningWithBlippedAgentDoesNotReprovision(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{phase: ports.InstanceRunning} // container still alive
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-live"
	w.AgentConnected = false // transient blip: hub momentarily reported disconnected
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if got.Phase != box.PhaseRunning {
		t.Fatalf("phase=%s want Running (no destructive re-provision on blip)", got.Phase)
	}
	if comp.provisioned != 0 {
		t.Fatalf("expected NO re-provision of a live workspace, got provisioned=%d", comp.provisioned)
	}
	if comp.destroyed != 0 {
		t.Fatalf("expected NO destroy of a live workspace, got destroyed=%d", comp.destroyed)
	}
}

func TestRunningExposesIngressIdempotently(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	ig := &fakeIngress{}
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, ig, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseRunning
	w.InstanceRef = "c-x"
	w.AgentConnected = true
	w.Ingress = []workspace.IngressPort{{Name: "app", Port: 3000}}
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkspace(ctx, "default", w.ID)
	if len(got.Endpoints) != 1 || got.Endpoints[0].URL != "https://app-"+w.ID+".gw" {
		t.Fatalf("endpoint not resolved: %+v", got.Endpoints)
	}
	// second tick must NOT re-expose (idempotent: endpoint already recorded)
	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	if ig.exposed != 1 {
		t.Fatalf("expose called %d times, want 1 (idempotent)", ig.exposed)
	}
}

func TestDestroyingUnexposesEndpoints(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	ig := &fakeIngress{}
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, ig, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseDestroying
	w.InstanceRef = "c-1"
	w.Endpoints = []workspace.Endpoint{{Name: "app", Ref: "app-x.gw", URL: "https://app-x.gw", Port: 3000}}
	_ = st.CreateWorkspace(ctx, w)

	if err := r.ReconcileOne(ctx, w.ID, "default"); err != nil {
		t.Fatal(err)
	}
	if ig.unexposed != 1 {
		t.Fatalf("unexpose called %d times, want 1", ig.unexposed)
	}
}

func TestDestroyingRemoves(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	comp := &fakeCompute{}
	r := reconciler.New(st, comp, &fakeStorage{}, nil, reconciler.Config{})

	w := workspace.New("default", "alice", "proj", "ubuntu:24.04")
	w.Phase = box.PhaseDestroying
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
