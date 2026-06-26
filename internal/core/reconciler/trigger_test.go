package reconciler_test

import (
	"context"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/reconciler"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// A Trigger must drive a single workspace immediately, without waiting for the
// poll tick. We set a very long interval so the ticker cannot be the cause: if
// the workspace reconciles, it was the event path.
func TestTriggerReconcilesBeforeTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := newStore(t)
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, nil, reconciler.Config{Interval: time.Hour})

	w := workspace.New("default", "alice", "proj", "img")
	_ = st.CreateWorkspace(ctx, w) // Pending

	go r.Run(ctx)
	r.Trigger(w.ID, "default")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := st.GetWorkspace(ctx, "default", w.ID)
		if got.Phase == box.PhaseProvisioning {
			return // event path reconciled it well before the 1h tick
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Trigger did not reconcile workspace before the tick")
}

// Trigger must never block, even when nothing is draining yet / buffer pressure.
func TestTriggerNonBlocking(t *testing.T) {
	st := newStore(t)
	r := reconciler.New(st, &fakeCompute{}, &fakeStorage{}, nil, reconciler.Config{Interval: time.Hour})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			r.Trigger("w", "default")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Trigger blocked")
	}
}
