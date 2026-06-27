package boxsqlite

import (
	"context"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/box.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRoundTripCreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	b := box.New("default", "alice", "proj", "alpine")
	b.Backend = "docker"
	b.MemMB, b.CPUMillis = 2048, 2000
	b.Ephemeral, b.Grace = true, 5*time.Minute
	b.Load = 0.42
	b.LastActive = time.Now().UTC().Round(0)
	b.BootstrapToken = "tok-1"
	if err := s.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetByName(ctx, "default", "proj")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != b.ID || got.Owner != "alice" || got.ImageRef != "alpine" || got.Backend != "docker" {
		t.Fatalf("metadata not persisted: %+v", got)
	}
	if got.MemMB != 2048 || got.CPUMillis != 2000 {
		t.Fatalf("caps not persisted: mem=%d cpu=%d", got.MemMB, got.CPUMillis)
	}
	if !got.Ephemeral || got.Grace != 5*time.Minute {
		t.Fatalf("lifetime not persisted: ephemeral=%v grace=%v", got.Ephemeral, got.Grace)
	}
	if got.Load != 0.42 || !got.LastActive.Equal(b.LastActive) {
		t.Fatalf("activity not persisted: load=%v last_active=%v", got.Load, got.LastActive)
	}

	// token lookup (hub resolver path)
	byTok, err := s.GetByToken(ctx, "tok-1")
	if err != nil || byTok.ID != b.ID {
		t.Fatalf("GetByToken: %v / %v", byTok, err)
	}

	// update: stamp a deadline + phase, ensure it survives a reload.
	d := time.Now().UTC().Add(time.Minute).Round(0)
	got.Deadline = &d
	got.Phase = box.PhaseRunning
	got.Attached = true
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := s.Get(ctx, "default", b.ID)
	if re.Phase != box.PhaseRunning || !re.Attached || re.Deadline == nil || !re.Deadline.Equal(d) {
		t.Fatalf("update not persisted: %+v", re)
	}

	if err := s.Delete(ctx, "default", b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "default", b.ID); err != box.ErrNotFound {
		t.Fatalf("deleted box should be gone, got %v", err)
	}
}

func TestListAndNotFound(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, err := s.GetByName(ctx, "default", "nope"); err != box.ErrNotFound {
		t.Fatalf("missing box must be ErrNotFound, got %v", err)
	}
	for _, n := range []string{"a", "b"} {
		_ = s.Create(ctx, box.New("default", "alice", n, "alpine"))
	}
	all, err := s.List(ctx, "") // sweep: all tenants
	if err != nil || len(all) != 2 {
		t.Fatalf("List(\"\") = %d boxes, err=%v; want 2", len(all), err)
	}
}

// Persistence across "restart": a second Store over the same file sees the box.
func TestSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s1, err := Open(dir + "/box.db")
	if err != nil {
		t.Fatal(err)
	}
	b := box.New("default", "alice", "keep", "alpine")
	if err := s1.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := Open(dir + "/box.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.GetByName(ctx, "default", "keep")
	if err != nil || got.ID != b.ID {
		t.Fatalf("box did not survive reopen: %v / %v", got, err)
	}
}
