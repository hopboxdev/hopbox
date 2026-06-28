package box

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EngineConfig holds the box-spawn defaults the Engine applies.
type EngineConfig struct {
	Tenant        string                  // single-tenant id boxes are created under
	DefaultImage  string                  // image when the spec names none
	Backends      []string                // compute backends actually configured (for ResolveBackend)
	DefBackend    string                  // default backend when more than one is configured
	DefaultFlavor Flavor                  // resource caps applied to a box unless its spec names a known flavor
	Persistent    func(owner string) bool // owner -> persistent (auto-suspend) vs ephemeral reap; nil = all ephemeral
	DefaultGrace  time.Duration           // ephemeral reconnect window after detach before reap (0 = reap immediately)
}

// Engine is the box product's core service: spawn / attach / inspect / destroy a
// box, over a Store and a reconcile wake-up. It is the surface a box-only daemon
// exposes and the SSH front door drives — and it knows nothing about the dev-env.
type Engine struct {
	store Store
	wake  func(id, tenant string) // reconcile trigger; nil = rely on the interval sweep
	cfg   EngineConfig
}

// NewEngine builds a box Engine. wake may be nil.
func NewEngine(s Store, wake func(id, tenant string), cfg EngineConfig) *Engine {
	if wake == nil {
		wake = func(string, string) {}
	}
	return &Engine{store: s, wake: wake, cfg: cfg}
}

// build turns a spec into a desired ephemeral Box: resolves the backend, applies
// the default image and the resource flavor (a known named flavor in the spec
// overrides the engine default), and marks it temporary.
func (e *Engine) build(owner string, spec Spec) (*Box, error) {
	if spec.Special != "" {
		return nil, fmt.Errorf("special username %q spawns no box", spec.Special)
	}
	backend, err := ResolveBackend(spec.Backend, e.cfg.Backends, e.cfg.DefBackend)
	if err != nil {
		return nil, err
	}
	image := spec.Image
	if image == "" {
		image = e.cfg.DefaultImage
	}
	b := New(e.cfg.Tenant, owner, spec.Name, image)
	b.Backend = backend
	// Tier: an identified owner gets a persistent, auto-suspending box (krillbox
	// model); an anonymous owner gets an ephemeral reap-on-disconnect box.
	if e.cfg.Persistent != nil && e.cfg.Persistent(owner) {
		b.AutoSuspend = true
	} else {
		b.Ephemeral = true
	}
	b.Grace = spec.Grace
	if b.Ephemeral && b.Grace == 0 {
		b.Grace = e.cfg.DefaultGrace // a brief reconnect/blip window before reaping
	}
	fl := e.cfg.DefaultFlavor
	if named, ok := ResolveFlavor(spec.Flavor); ok {
		fl = named
	}
	b.Apply(fl)
	return b, nil
}

// Attach resolves the box a username names for owner, creating it (ephemeral) if
// absent, marks it attached, and returns a release that detaches on session end.
// The box is owned by owner; attaching to another owner's box is refused.
func (e *Engine) Attach(ctx context.Context, owner, username string) (*Box, func(), error) {
	spec, err := ParseSpec(username)
	if err != nil {
		return nil, nil, err
	}
	if spec.Special != "" {
		return nil, nil, fmt.Errorf("username %q does not spawn a box", username)
	}

	// Box names are per-owner: your "myproject" is distinct from another user's.
	b, err := e.byOwnerName(ctx, owner, spec.Name)
	switch {
	case err == nil: // own box -> reattach below
	case errors.Is(err, ErrNotFound):
		b, err = e.build(owner, spec)
		if err != nil {
			return nil, nil, err
		}
		b.Attached = true
		if err := e.store.Create(ctx, b); err != nil {
			return nil, nil, fmt.Errorf("create box: %w", err)
		}
		e.wake(b.ID, e.cfg.Tenant)
		return b, e.releaser(b.ID), nil
	default:
		return nil, nil, err
	}

	// Reuse path: re-attach an existing box.
	b.Attached = true
	if err := e.store.Update(ctx, b); err != nil {
		return nil, nil, fmt.Errorf("attach box: %w", err)
	}
	e.wake(b.ID, e.cfg.Tenant)
	return b, e.releaser(b.ID), nil
}

// byOwnerName finds an owner's box by name (the box namespace is per-owner).
func (e *Engine) byOwnerName(ctx context.Context, owner, name string) (*Box, error) {
	all, err := e.store.List(ctx, e.cfg.Tenant)
	if err != nil {
		return nil, err
	}
	for _, b := range all {
		if b.Owner == owner && b.Name == name {
			return b, nil
		}
	}
	return nil, ErrNotFound
}

// releaser returns the session-end hook: clear Attached and wake the reconciler
// so an ephemeral box is reaped promptly. Best-effort. Keyed by id (names are
// per-owner, so a name lookup would be ambiguous).
func (e *Engine) releaser(id string) func() {
	return func() {
		b, err := e.store.Get(context.Background(), e.cfg.Tenant, id)
		if err != nil {
			return // already gone
		}
		b.Attached = false
		b.LastActive = time.Now() // start the idle clock from detach (drives auto-suspend)
		if err := e.store.Update(context.Background(), b); err != nil {
			return
		}
		e.wake(id, e.cfg.Tenant)
	}
}

// Get / List return boxes for the engine's tenant.
func (e *Engine) Get(ctx context.Context, id string) (*Box, error) {
	return e.store.Get(ctx, e.cfg.Tenant, id)
}
func (e *Engine) List(ctx context.Context) ([]*Box, error) {
	return e.store.List(ctx, e.cfg.Tenant)
}

// Destroy flags a box for teardown; the reconciler does the actual reap.
func (e *Engine) Destroy(ctx context.Context, id string) error {
	b, err := e.store.Get(ctx, e.cfg.Tenant, id)
	if err != nil {
		return err
	}
	b.Phase = PhaseDestroying
	if err := e.store.Update(ctx, b); err != nil {
		return err
	}
	e.wake(id, e.cfg.Tenant)
	return nil
}
