package box

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

func TestLifetimePersistentNeverReaps(t *testing.T) {
	b := New("default", "alice", "proj", "img")
	b.Attached = false
	got := b.EvalLifetime(t0)
	if got.Reap || got.SetDeadline != nil || got.ClearDeadline {
		t.Fatalf("persistent box must be lifetime no-op, got %+v", got)
	}
}

func TestLifetimeDieOnDisconnect(t *testing.T) {
	// Ephemeral, grace 0: detached agent reaps immediately.
	b := New("default", "alice", "proj", "img")
	b.Ephemeral = true
	b.Attached = false
	if got := b.EvalLifetime(t0); !got.Reap {
		t.Fatalf("grace=0 detached must reap, got %+v", got)
	}
}

func TestLifetimeConnectedStaysAlive(t *testing.T) {
	b := New("default", "alice", "proj", "img")
	b.Ephemeral = true
	b.Attached = true
	if got := b.EvalLifetime(t0); got.Reap {
		t.Fatal("connected ephemeral box must not reap")
	}
}

func TestLifetimeGraceStampsDeadlineThenReaps(t *testing.T) {
	b := New("default", "alice", "proj", "img")
	b.Ephemeral = true
	b.Grace = 5 * time.Minute
	b.Attached = false

	// first eval: no deadline yet -> stamp now+grace, do not reap.
	got := b.EvalLifetime(t0)
	if got.Reap || got.SetDeadline == nil {
		t.Fatalf("first detached eval must stamp deadline without reaping, got %+v", got)
	}
	want := t0.Add(5 * time.Minute)
	if !got.SetDeadline.Equal(want) {
		t.Fatalf("deadline=%v want %v", got.SetDeadline, want)
	}

	// deadline now persisted on the box.
	b.Deadline = got.SetDeadline

	// still within grace: no reap.
	if got := b.EvalLifetime(t0.Add(4 * time.Minute)); got.Reap {
		t.Fatal("must not reap before deadline")
	}
	// past deadline: reap.
	if got := b.EvalLifetime(t0.Add(6 * time.Minute)); !got.Reap {
		t.Fatal("must reap once past deadline")
	}
}

func TestLifetimeReconnectClearsDeadline(t *testing.T) {
	b := New("default", "alice", "proj", "img")
	b.Ephemeral = true
	b.Grace = 5 * time.Minute
	d := t0.Add(5 * time.Minute)
	b.Deadline = &d
	b.Attached = true // reconnected within grace

	got := b.EvalLifetime(t0.Add(time.Minute))
	if got.Reap {
		t.Fatal("reconnected box must not reap")
	}
	if !got.ClearDeadline {
		t.Fatal("reconnect must clear the pending deadline")
	}
}

func TestLifetimeMaxTTLReapsEvenWhenConnected(t *testing.T) {
	// Hard cap (e.g. a tier timeout) reaps regardless of connection / orphan state.
	b := New("default", "alice", "proj", "img")
	b.Ephemeral = true
	b.MaxTTL = time.Hour
	b.Attached = true
	b.CreatedAt = t0
	if got := b.EvalLifetime(t0.Add(time.Hour + time.Second)); !got.Reap {
		t.Fatal("max_ttl must reap even a connected box")
	}
	if got := b.EvalLifetime(t0.Add(30 * time.Minute)); got.Reap {
		t.Fatal("must not reap before max_ttl")
	}
}
