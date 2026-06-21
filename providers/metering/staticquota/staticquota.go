// Package staticquota is the config-defined Metering provider: a fixed
// per-principal workspace limit enforced as a pre-flight Quota gate. Emit tracks
// active workspaces from workspace.start/stop events; Quota reports whether the
// principal may provision another. It is the zero-dependency default; the
// prometheus provider (post-MVP) emits usage as metrics instead.
package staticquota

import (
	"context"
	"sync"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// event kinds the active-workspace counter reacts to.
const (
	kindStart   = "workspace.start"
	kindStop    = "workspace.stop"
	kindDestroy = "workspace.destroy"
)

// Provider counts active workspaces per principal and gates on a fixed limit.
// limit <= 0 means unlimited. Safe for concurrent use.
type Provider struct {
	limit  int64
	mu     sync.Mutex
	active map[string]int64 // principal id -> active workspace count
}

var _ ports.Metering = (*Provider)(nil)

// New builds a static-quota provider allowing up to workspacesLimit active
// workspaces per principal (<= 0 means unlimited).
func New(workspacesLimit int64) *Provider {
	return &Provider{limit: workspacesLimit, active: map[string]int64{}}
}

func (p *Provider) Emit(_ context.Context, e ports.UsageEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch e.Kind {
	case kindStart:
		p.active[e.PrincipalID]++
	case kindStop, kindDestroy:
		if p.active[e.PrincipalID] > 0 {
			p.active[e.PrincipalID]--
		}
	}
	return nil
}

func (p *Provider) Quota(_ context.Context, r ports.PrincipalRef) (ports.QuotaState, error) {
	p.mu.Lock()
	used := p.active[r.PrincipalID]
	p.mu.Unlock()

	if p.limit <= 0 { // unlimited
		return ports.QuotaState{Allowed: true, WorkspacesUsed: used, WorkspacesLimit: 0}, nil
	}
	if used < p.limit {
		return ports.QuotaState{Allowed: true, WorkspacesUsed: used, WorkspacesLimit: p.limit}, nil
	}
	return ports.QuotaState{
		Allowed: false, WorkspacesUsed: used, WorkspacesLimit: p.limit,
		Reason: "workspace limit reached",
	}, nil
}
