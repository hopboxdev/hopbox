package workspace

import "testing"

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to Phase
		ok       bool
	}{
		{PhasePending, PhaseProvisioning, true},
		{PhaseProvisioning, PhaseRunning, true},
		{PhaseProvisioning, PhaseFailed, true},
		{PhaseRunning, PhaseProvisioning, true}, // self-heal: agent dropped
		{PhaseRunning, PhaseDestroying, true},
		{PhasePending, PhaseRunning, false}, // must provision first
		{PhaseFailed, PhaseRunning, false},
		{PhaseDestroying, PhaseRunning, false},
		{PhaseStopped, PhaseProvisioning, true},  // can resume a stopped workspace
		{PhaseStopped, PhaseRunning, false},      // must go through Provisioning first
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.ok {
			t.Errorf("CanTransition(%s,%s)=%v want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	w := New("default", "alice", "proj", "ubuntu:24.04")
	if w.Phase != PhasePending {
		t.Fatalf("phase=%s want Pending", w.Phase)
	}
	if w.ID == "" || w.TenantID != "default" || w.Owner != "alice" || w.ImageRef != "ubuntu:24.04" {
		t.Fatalf("bad workspace: %+v", w)
	}
}
