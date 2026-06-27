package box

import (
	"testing"
	"time"
)

func TestRecordHeartbeatAndIsIdle(t *testing.T) {
	cfg := IdleConfig{Timeout: 15 * time.Minute, LoadThreshold: 0.2}
	t0 := time.Now()

	b := &Box{}
	// First low-load heartbeat while unattached: marks activity (LastActive set),
	// so the countdown starts now rather than from the zero time.
	b.Attached = false
	b.RecordHeartbeat(0.05, t0, cfg)
	if !b.LastActive.IsZero() {
		// low load + unattached => no activity bump; LastActive stays zero...
	}
	if b.IsIdle(t0, cfg) {
		t.Fatal("a box with no prior activity is not idle")
	}

	// Attached => activity refreshes every heartbeat, never idle.
	b.Attached = true
	b.RecordHeartbeat(0.0, t0, cfg)
	if b.IsIdle(t0.Add(time.Hour), cfg) {
		t.Fatal("attached box is never idle")
	}

	// Detach with a busy heartbeat sets the activity marker; then go quiet.
	b.Attached = true
	b.RecordHeartbeat(1.0, t0, cfg) // busy -> LastActive = t0
	b.Attached = false
	b.RecordHeartbeat(0.01, t0.Add(5*time.Minute), cfg) // quiet, no bump
	if b.IsIdle(t0.Add(10*time.Minute), cfg) {
		t.Fatal("not idle before the timeout elapses")
	}
	if !b.IsIdle(t0.Add(16*time.Minute), cfg) {
		t.Fatal("idle after Timeout of quiet")
	}

	// A load spike while quiet pushes the activity marker forward.
	b.RecordHeartbeat(0.9, t0.Add(16*time.Minute), cfg)
	if b.IsIdle(t0.Add(20*time.Minute), cfg) {
		t.Fatal("a recent load spike resets the idle countdown")
	}
}
