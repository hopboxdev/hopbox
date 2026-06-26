package sshfront_test

import (
	"context"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
	"github.com/hopboxdev/hopbox/internal/sshfront"
)

// fakeStore is an in-memory Store keyed by (tenant,name).
type fakeStore struct {
	byName map[string]*workspace.Workspace
}

func newFakeStore() *fakeStore { return &fakeStore{byName: map[string]*workspace.Workspace{}} }

func (s *fakeStore) GetByName(_ context.Context, tenant, name string) (*workspace.Workspace, error) {
	if w, ok := s.byName[tenant+"/"+name]; ok {
		return w, nil
	}
	return nil, store.ErrNotFound
}
func (s *fakeStore) CreateWorkspace(_ context.Context, w *workspace.Workspace) error {
	s.byName[w.TenantID+"/"+w.Name] = w
	return nil
}
func (s *fakeStore) UpdateWorkspace(_ context.Context, w *workspace.Workspace) error {
	s.byName[w.TenantID+"/"+w.Name] = w
	return nil
}

func newManager(t *testing.T, st sshfront.Store) (*sshfront.Manager, *[]string) {
	t.Helper()
	var triggered []string
	m := sshfront.New(st, func(id, tenant string) { triggered = append(triggered, id) }, sshfront.Config{
		Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"},
	})
	return m, &triggered
}

func TestAttachCreatesEphemeralWorkspace(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m, triggered := newManager(t, st)

	w, release, err := m.Attach(ctx, "alice", "proj:python+5m")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if w.Name != "proj" || w.ImageRef != "python" || w.Owner != "alice" {
		t.Fatalf("workspace: name=%q image=%q owner=%q", w.Name, w.ImageRef, w.Owner)
	}
	if !w.Ephemeral || w.Grace != 5*time.Minute {
		t.Fatalf("expected ephemeral grace=5m, got ephemeral=%v grace=%v", w.Ephemeral, w.Grace)
	}
	if !w.Attached {
		t.Fatal("a freshly attached workspace must be marked Attached")
	}
	if w.Backend != "docker" {
		t.Fatalf("backend=%q want docker (sole backend)", w.Backend)
	}
	if len(*triggered) == 0 {
		t.Fatal("Attach must trigger a reconcile")
	}
}

func TestAttachAppliesFlavorCap(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m := sshfront.New(st, nil, sshfront.Config{
		Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"},
		DefaultMemMB: 2048, DefaultCPUMillis: 2000,
	})
	w, release, err := m.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if w.MemMB != 2048 || w.CPUMillis != 2000 {
		t.Fatalf("front-door box must inherit the default caps: mem=%d cpu=%d want 2048/2000", w.MemMB, w.CPUMillis)
	}
}

func TestAttachNamedFlavorOverridesDefault(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m := sshfront.New(st, nil, sshfront.Config{
		Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"},
		DefaultMemMB: 1024, DefaultCPUMillis: 1000,
	})
	// spec names the "large" built-in flavor -> overrides the configured defaults.
	w, release, err := m.Attach(ctx, "alice", "proj:alpine:large")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if w.MemMB != 4096 || w.CPUMillis != 4000 {
		t.Fatalf("named flavor must override defaults: mem=%d cpu=%d want 4096/4000", w.MemMB, w.CPUMillis)
	}
}

func TestAttachReusesExistingWorkspace(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m, _ := newManager(t, st)

	w1, rel1, err := m.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	rel1()
	w2, rel2, err := m.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	defer rel2()
	if w1.ID != w2.ID {
		t.Fatalf("reconnect must reuse the same workspace: %s != %s", w1.ID, w2.ID)
	}
	if !w2.Attached {
		t.Fatal("reused workspace must be re-attached")
	}
}

func TestAttachRejectsForeignOwner(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m, _ := newManager(t, st)

	if _, rel, err := m.Attach(ctx, "alice", "proj"); err != nil {
		t.Fatal(err)
	} else {
		rel()
	}
	// bob may not attach to alice's workspace.
	if _, _, err := m.Attach(ctx, "bob", "proj"); err == nil {
		t.Fatal("attaching to another principal's workspace must error")
	}
}

func TestReleaseDetaches(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m, triggered := newManager(t, st)

	w, release, err := m.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	before := len(*triggered)
	release()

	got, _ := st.GetByName(ctx, "default", "proj")
	if got.Attached {
		t.Fatal("release must clear Attached so the reconciler can reap")
	}
	if len(*triggered) <= before {
		t.Fatal("release must trigger a reconcile so the reap happens promptly")
	}
	_ = w
}

func TestAttachRejectsSpecialUsername(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	m, _ := newManager(t, st)
	if _, _, err := m.Attach(ctx, "alice", "cli"); err == nil {
		t.Fatal("special username (cli) must not spawn a box via Attach")
	}
}
