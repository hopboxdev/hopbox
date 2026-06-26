package box

import (
	"context"
	"testing"
	"time"
)

// fakeStore is an in-memory box.Store keyed by (tenant,name) and id.
type fakeStore struct {
	byName map[string]*Box
	byID   map[string]*Box
}

func newFakeStore() *fakeStore {
	return &fakeStore{byName: map[string]*Box{}, byID: map[string]*Box{}}
}
func (s *fakeStore) put(b *Box) { s.byName[b.TenantID+"/"+b.Name] = b; s.byID[b.TenantID+"/"+b.ID] = b }
func (s *fakeStore) Get(_ context.Context, tenant, id string) (*Box, error) {
	if b, ok := s.byID[tenant+"/"+id]; ok {
		return b, nil
	}
	return nil, ErrNotFound
}
func (s *fakeStore) GetByName(_ context.Context, tenant, name string) (*Box, error) {
	if b, ok := s.byName[tenant+"/"+name]; ok {
		return b, nil
	}
	return nil, ErrNotFound
}
func (s *fakeStore) List(_ context.Context, tenant string) ([]*Box, error) {
	var out []*Box
	for k, b := range s.byID {
		if len(k) > len(tenant) && k[:len(tenant)+1] == tenant+"/" {
			out = append(out, b)
		}
	}
	return out, nil
}
func (s *fakeStore) Create(_ context.Context, b *Box) error { s.put(b); return nil }
func (s *fakeStore) Update(_ context.Context, b *Box) error { s.put(b); return nil }
func (s *fakeStore) Delete(_ context.Context, tenant, id string) error {
	if b, ok := s.byID[tenant+"/"+id]; ok {
		delete(s.byID, tenant+"/"+id)
		delete(s.byName, tenant+"/"+b.Name)
	}
	return nil
}

func newEngine(st Store) (*Engine, *[]string) {
	var woke []string
	e := NewEngine(st, func(id, _ string) { woke = append(woke, id) }, EngineConfig{
		Tenant: "default", DefaultImage: "alpine", Backends: []string{"docker"},
		DefaultFlavor: Flavor{CPUMillis: 2000, MemMB: 2048},
	})
	return e, &woke
}

func TestEngineAttachCreatesEphemeralBox(t *testing.T) {
	ctx := context.Background()
	e, woke := newEngine(newFakeStore())
	b, release, err := e.Attach(ctx, "alice", "proj:python+5m")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if b.Name != "proj" || b.ImageRef != "python" || b.Owner != "alice" || b.Backend != "docker" {
		t.Fatalf("box: %+v", b)
	}
	if !b.Ephemeral || b.Grace != 5*time.Minute || !b.Attached {
		t.Fatalf("ephemeral/grace/attached wrong: %+v", b)
	}
	if b.MemMB != 2048 || b.CPUMillis != 2000 {
		t.Fatalf("default flavor cap not applied: mem=%d cpu=%d", b.MemMB, b.CPUMillis)
	}
	if len(*woke) == 0 {
		t.Fatal("Attach must wake the reconciler")
	}
}

func TestEngineAttachNamedFlavorOverrides(t *testing.T) {
	e, _ := newEngine(newFakeStore())
	b, release, err := e.Attach(context.Background(), "alice", "proj:alpine:large")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if b.MemMB != 4096 || b.CPUMillis != 4000 {
		t.Fatalf("named flavor must override default: mem=%d cpu=%d", b.MemMB, b.CPUMillis)
	}
}

func TestEngineAttachReusesAndRejectsForeignOwner(t *testing.T) {
	ctx := context.Background()
	e, _ := newEngine(newFakeStore())
	b1, rel1, err := e.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	rel1()
	b2, rel2, err := e.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	defer rel2()
	if b1.ID != b2.ID || !b2.Attached {
		t.Fatalf("reconnect must reuse + re-attach: %s/%s attached=%v", b1.ID, b2.ID, b2.Attached)
	}
	if _, _, err := e.Attach(ctx, "bob", "proj"); err == nil {
		t.Fatal("attaching to another owner's box must error")
	}
}

func TestEngineReleaseDetaches(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	e, woke := newEngine(st)
	_, release, err := e.Attach(ctx, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	before := len(*woke)
	release()
	got, _ := st.GetByName(ctx, "default", "proj")
	if got.Attached {
		t.Fatal("release must clear Attached")
	}
	if len(*woke) <= before {
		t.Fatal("release must wake the reconciler")
	}
}

func TestEngineAttachRejectsSpecial(t *testing.T) {
	e, _ := newEngine(newFakeStore())
	if _, _, err := e.Attach(context.Background(), "alice", "cli"); err == nil {
		t.Fatal("special username (cli) must not spawn a box")
	}
}

func TestEngineDestroyFlagsAndWakes(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	e, woke := newEngine(st)
	b, release, _ := e.Attach(ctx, "alice", "proj")
	release()
	before := len(*woke)
	if err := e.Destroy(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Get(ctx, b.ID)
	if got.Phase != PhaseDestroying {
		t.Fatalf("phase=%s want Destroying", got.Phase)
	}
	if len(*woke) <= before {
		t.Fatal("Destroy must wake the reconciler")
	}
}
