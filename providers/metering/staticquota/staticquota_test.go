package staticquota

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func start(p *Provider, pid string) {
	_ = p.Emit(context.Background(), ports.UsageEvent{PrincipalID: pid, Kind: "workspace.start"})
}
func stop(p *Provider, pid string) {
	_ = p.Emit(context.Background(), ports.UsageEvent{PrincipalID: pid, Kind: "workspace.stop"})
}
func quota(p *Provider, pid string) ports.QuotaState {
	q, _ := p.Quota(context.Background(), ports.PrincipalRef{PrincipalID: pid})
	return q
}

func TestLimitEnforcedAndReleased(t *testing.T) {
	p := New(2)
	if q := quota(p, "alice"); !q.Allowed || q.WorkspacesUsed != 0 {
		t.Fatalf("fresh principal must be allowed: %+v", q)
	}
	start(p, "alice")
	if q := quota(p, "alice"); !q.Allowed || q.WorkspacesUsed != 1 {
		t.Fatalf("1<2 must be allowed: %+v", q)
	}
	start(p, "alice")
	q := quota(p, "alice")
	if q.Allowed || q.WorkspacesUsed != 2 || q.Reason == "" {
		t.Fatalf("at limit must be denied with reason: %+v", q)
	}
	// stopping a workspace frees quota again
	stop(p, "alice")
	if q := quota(p, "alice"); !q.Allowed || q.WorkspacesUsed != 1 {
		t.Fatalf("after stop must be allowed: %+v", q)
	}
	// counter floors at zero; isolation between principals
	stop(p, "alice")
	stop(p, "alice")
	if q := quota(p, "alice"); q.WorkspacesUsed != 0 {
		t.Fatalf("counter must floor at 0: %+v", q)
	}
	if q := quota(p, "bob"); !q.Allowed || q.WorkspacesUsed != 0 {
		t.Fatalf("principals must be isolated: %+v", q)
	}
}

func TestUnlimitedWhenLimitZero(t *testing.T) {
	p := New(0)
	for i := 0; i < 100; i++ {
		start(p, "alice")
	}
	if q := quota(p, "alice"); !q.Allowed || q.WorkspacesLimit != 0 {
		t.Fatalf("limit<=0 means unlimited: %+v", q)
	}
}
