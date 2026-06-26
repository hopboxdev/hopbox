// Package sshfront is the krillbox-style SSH front door: an SSH username is a
// workspace spec and the client's key is its identity. The Manager runs the
// session -> spec -> teardown loop — it resolves (or creates) the ephemeral
// workspace a username names, marks it attached for the life of the session,
// and detaches it on disconnect so the reconciler reaps it.
package sshfront

import (
	"context"
	"errors"
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// Store is the slice of the state store the front door needs.
type Store interface {
	GetByName(ctx context.Context, tenant, name string) (*workspace.Workspace, error)
	CreateWorkspace(ctx context.Context, w *workspace.Workspace) error
	UpdateWorkspace(ctx context.Context, w *workspace.Workspace) error
}

// Config holds the front door's workspace-creation defaults.
type Config struct {
	Tenant           string   // single-tenant id the front door creates under
	DefaultImage     string   // image used when the username names none
	Backends         []string // compute backends actually configured (for ResolveBackend)
	DefBackend       string   // default backend when more than one is configured
	DefaultMemMB     int64    // memory cap (MB) applied to every front-door box; 0 = unlimited
	DefaultCPUMillis int64    // CPU cap (milli-cores) applied to every front-door box; 0 = unlimited
}

// Manager owns the attach/detach lifecycle for SSH front-door sessions.
type Manager struct {
	store   Store
	trigger func(workspaceID, tenant string) // reconcile wake-up (events bus Publish)
	cfg     Config
}

func New(s Store, trigger func(workspaceID, tenant string), cfg Config) *Manager {
	if trigger == nil {
		trigger = func(string, string) {}
	}
	return &Manager{store: s, trigger: trigger, cfg: cfg}
}

// Attach resolves the workspace a username names for this principal, creating it
// (ephemeral) if absent, marks it attached, and returns a release that detaches
// on session end. The returned workspace is owned by principal; attaching to
// another principal's workspace is refused.
func (m *Manager) Attach(ctx context.Context, principal, username string) (*workspace.Workspace, func(), error) {
	spec, err := box.ParseSpec(username)
	if err != nil {
		return nil, nil, err
	}
	if spec.Special != "" {
		return nil, nil, fmt.Errorf("username %q does not spawn a workspace", username)
	}
	tenant := m.cfg.Tenant

	w, err := m.store.GetByName(ctx, tenant, spec.Name)
	switch {
	case err == nil:
		if w.Owner != principal {
			return nil, nil, fmt.Errorf("workspace %q belongs to another user", spec.Name)
		}
	case errors.Is(err, store.ErrNotFound):
		w, err = workspace.BuildFromSpec(spec, tenant, principal, m.cfg.DefaultImage, m.cfg.Backends, m.cfg.DefBackend)
		if err != nil {
			return nil, nil, err
		}
		// Cap anonymous front-door boxes (cpu + mem) so one can't exhaust the host.
		// A recognized named flavor in the spec (`name:img:medium`) overrides the
		// configured default; an unknown flavor name falls back to it.
		fl := box.Flavor{CPUMillis: m.cfg.DefaultCPUMillis, MemMB: m.cfg.DefaultMemMB}
		if named, ok := box.ResolveFlavor(spec.Flavor); ok {
			fl = named
		}
		w.Apply(fl)
		w.Attached = true
		if err := m.store.CreateWorkspace(ctx, w); err != nil {
			return nil, nil, fmt.Errorf("create workspace: %w", err)
		}
		m.trigger(w.ID, tenant)
		return w, m.releaser(w.ID, tenant, spec.Name), nil
	default:
		return nil, nil, err
	}

	// Reuse path: re-attach an existing workspace.
	w.Attached = true
	if err := m.store.UpdateWorkspace(ctx, w); err != nil {
		return nil, nil, fmt.Errorf("attach workspace: %w", err)
	}
	m.trigger(w.ID, tenant)
	return w, m.releaser(w.ID, tenant, spec.Name), nil
}

// releaser returns the session-end hook: it clears Attached and wakes the
// reconciler so an ephemeral box is reaped promptly. Best-effort — a dropped
// detach only delays the reap until the reconciler's interval sweep.
func (m *Manager) releaser(id, tenant, name string) func() {
	return func() {
		w, err := m.store.GetByName(context.Background(), tenant, name)
		if err != nil || w.ID != id {
			return // already gone / replaced
		}
		w.Attached = false
		if err := m.store.UpdateWorkspace(context.Background(), w); err != nil {
			return
		}
		m.trigger(id, tenant)
	}
}
