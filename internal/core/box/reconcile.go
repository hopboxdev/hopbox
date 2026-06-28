package box

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// ReconcileConfig configures the box reconcile loop.
type ReconcileConfig struct {
	AgentAddr string           // address the in-box agent dials back (HOPBOX_CONTROL_ADDR)
	Agent     ports.AgentImage // how to side-load the agent binary
	MetaURL   string           // box metadata API URL injected as $BOX_META; "" = none
	GuestBin  string           // host path of box-guest to side-load into each box; "" = none
	Idle      IdleConfig       // when a persistent AutoSuspend box is considered idle
	Interval  time.Duration    // backstop sweep period (default 1s)
	Hooks     Hooks            // optional host-layer lifecycle hooks (dev-env: storage + ingress); nil = none
	Now       func() time.Time // clock seam; nil = time.Now
}

// Hooks lets a host layer extend the box lifecycle without box-core depending on
// it. The dev-env implements them for storage homes + ingress; boxd passes none.
// All are optional in effect — a nil ReconcileConfig.Hooks is a plain box.
type Hooks interface {
	// PreProvision runs before compute.Provision, contributing storage mounts and
	// extra env (e.g. the home mount + the box's SSH principal/CA/authorized keys).
	PreProvision(ctx context.Context, b *Box) (mounts []ports.Mount, env map[string]string, err error)
	// PostRunning runs each tick a box is Running with its agent connected — used
	// to reconcile ingress endpoints. Must be idempotent.
	PostRunning(ctx context.Context, b *Box) error
	// PreDestroy runs before teardown (e.g. ingress unexpose). Best-effort.
	PreDestroy(ctx context.Context, b *Box) error
}

// Reconciler drives boxes from spec to running and reaps ephemeral ones — the
// box-only counterpart of the dev-env reconciler, with no storage-home or ingress.
// It works over box.Store + ports.Compute, so a box-only daemon needs nothing
// from the dev-env layer.
type Reconciler struct {
	store   Store
	compute ports.Compute
	cfg     ReconcileConfig
	now     func() time.Time
	events  chan reconcileReq
}

type reconcileReq struct{ tenant, id string }

func NewReconciler(s Store, c ports.Compute, cfg ReconcileConfig) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = time.Second
	}
	if cfg.Idle == (IdleConfig{}) {
		cfg.Idle = DefaultIdle
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Reconciler{store: s, compute: c, cfg: cfg, now: now, events: make(chan reconcileReq, 1024)}
}

// Trigger converges one box now instead of waiting for the sweep. Never blocks.
func (r *Reconciler) Trigger(id, tenant string) {
	select {
	case r.events <- reconcileReq{tenant: tenant, id: id}:
	default:
	}
}

// Run drives the hybrid loop until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-r.events:
			if err := r.ReconcileOne(ctx, req.tenant, req.id); err != nil {
				log.Printf("boxreconcile: %s (event): %v", req.id, err)
			}
		case <-t.C:
			all, err := r.store.List(ctx, "")
			if err != nil {
				log.Printf("boxreconcile: list: %v", err)
				continue
			}
			for _, b := range all {
				if err := r.ReconcileOne(ctx, b.TenantID, b.ID); err != nil {
					log.Printf("boxreconcile: %s: %v", b.ID, err)
				}
			}
		}
	}
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ReconcileOne advances a single box one step toward its desired state.
func (r *Reconciler) ReconcileOne(ctx context.Context, tenant, id string) error {
	b, err := r.store.Get(ctx, tenant, id)
	if err != nil {
		return err
	}
	switch b.Phase {
	case PhasePending:
		return r.provision(ctx, b)
	case PhaseProvisioning:
		if b.AgentConnected {
			return r.setPhase(ctx, b, PhaseRunning, "agent connected")
		}
		return r.checkAlive(ctx, b)
	case PhaseRunning:
		if b.Ephemeral {
			done, err := r.reconcileLifetime(ctx, b)
			if done || err != nil {
				return err
			}
		}
		// A persistent, idle box suspends to disk (vs an ephemeral box's reap).
		if r.shouldSuspend(b) {
			return r.suspend(ctx, b)
		}
		if !b.AgentConnected {
			return r.healIfDead(ctx, b)
		}
		// Running + connected: let the host layer reconcile ingress endpoints.
		if r.cfg.Hooks != nil {
			return r.cfg.Hooks.PostRunning(ctx, b)
		}
		return nil
	case PhaseSuspended:
		// Resume when an owner attaches; otherwise stay snapshotted (a suspended
		// box's agent disconnect is expected, not a death to heal).
		if b.Attached {
			return r.resume(ctx, b)
		}
		return nil
	case PhaseDestroying:
		return r.destroy(ctx, b)
	default:
		return nil // Failed/Stopped terminal
	}
}

// shouldSuspend reports whether a running box should auto-suspend now.
func (r *Reconciler) shouldSuspend(b *Box) bool {
	if b.Ephemeral || !b.AutoSuspend || !b.AgentConnected {
		return false
	}
	if _, ok := r.compute.(ports.Suspender); !ok {
		return false
	}
	now := r.now()
	return now.After(b.KeepAliveUntil) && b.IsIdle(now, b.EffectiveIdle(r.cfg.Idle))
}

func (r *Reconciler) suspend(ctx context.Context, b *Box) error {
	susp := r.compute.(ports.Suspender) // guarded by shouldSuspend
	if err := susp.Suspend(ctx, b.InstanceRef); err != nil {
		return fmt.Errorf("suspend %s: %w", b.InstanceRef, err)
	}
	b.AgentConnected = false
	log.Printf("boxreconcile: %s (%s) auto-suspended (idle)", b.ID, b.Name)
	return r.setPhase(ctx, b, PhaseSuspended, "auto-suspended (idle)")
}

// Drain suspends every running, persistent box and marks it Suspended — for a
// graceful boxd shutdown, so a restart resumes boxes (disk + memory) instead of
// losing them. Best-effort; called once on shutdown with a fresh context.
func (r *Reconciler) Drain(ctx context.Context, tenant string) {
	susp, ok := r.compute.(ports.Suspender)
	if !ok {
		return
	}
	boxes, err := r.store.List(ctx, tenant)
	if err != nil {
		return
	}
	for _, b := range boxes {
		if b.Ephemeral || b.Phase != PhaseRunning {
			continue
		}
		if err := susp.Suspend(ctx, b.InstanceRef); err != nil {
			log.Printf("boxreconcile: drain %s (%s): %v", b.ID, b.Name, err)
			continue
		}
		b.AgentConnected = false
		_ = r.setPhase(ctx, b, PhaseSuspended, "suspended (shutdown)")
		log.Printf("boxreconcile: %s (%s) suspended for shutdown", b.ID, b.Name)
	}
}

func (r *Reconciler) resume(ctx context.Context, b *Box) error {
	susp, ok := r.compute.(ports.Suspender)
	if !ok {
		return r.provision(ctx, b) // can't resume; rebuild from spec
	}
	if err := susp.Resume(ctx, b.InstanceRef); err != nil {
		return fmt.Errorf("resume %s: %w", b.InstanceRef, err)
	}
	log.Printf("boxreconcile: %s (%s) resumed (attached)", b.ID, b.Name)
	// The box is running again; the agent reconnects shortly. AgentConnected
	// flips true when it does (PhaseRunning + !AgentConnected just polls Status).
	return r.setPhase(ctx, b, PhaseRunning, "resumed (attached)")
}

func (r *Reconciler) provision(ctx context.Context, b *Box) error {
	if b.BootstrapToken == "" {
		b.BootstrapToken = newToken()
	}
	env := map[string]string{
		"HOPBOX_AGENT_TOKEN":  b.BootstrapToken,
		"HOPBOX_CONTROL_ADDR": r.cfg.AgentAddr,
		"HOPBOX_WORKSPACE_ID": b.ID,
		"HOPBOX_PRINCIPAL":    b.Owner,
	}
	if r.cfg.MetaURL != "" {
		env["BOX_META"] = r.cfg.MetaURL // where box-guest reaches the metadata API
	}
	// The host layer (dev-env) contributes a storage home mount + extra env
	// (SSH host key on the mount, trusted CA, authorized keys). boxd has no hooks.
	var mounts []ports.Mount
	if r.cfg.Hooks != nil {
		m, hookEnv, err := r.cfg.Hooks.PreProvision(ctx, b)
		if err != nil {
			return r.fail(ctx, b, fmt.Errorf("pre-provision: %w", err))
		}
		mounts = m
		for k, v := range hookEnv {
			env[k] = v
		}
	}
	inst, err := r.compute.Provision(ctx, ports.ProvisionRequest{
		WorkspaceID: b.ID,
		ImageRef:    b.ImageRef,
		MemMB:       b.MemMB,
		CPUMillis:   b.CPUMillis,
		GuestBin:    r.cfg.GuestBin,
		Mounts:      mounts,
		Agent:       r.cfg.Agent,
		Env:         env,
	})
	if err != nil {
		return r.fail(ctx, b, fmt.Errorf("compute: %w", err))
	}
	b.InstanceRef = inst.Ref
	b.IP = inst.IP
	b.AgentConnected = false
	b.Phase = PhaseProvisioning
	b.Message = "provisioned, awaiting agent"
	return r.store.Update(ctx, b)
}

func (r *Reconciler) reconcileLifetime(ctx context.Context, b *Box) (bool, error) {
	act := b.EvalLifetime(r.now())
	if act.Reap {
		return true, r.setPhase(ctx, b, PhaseDestroying, "ephemeral: lifetime expired")
	}
	if act.SetDeadline != nil {
		b.Deadline = act.SetDeadline
		if err := r.store.Update(ctx, b); err != nil {
			return true, err
		}
	}
	if act.ClearDeadline {
		b.Deadline = nil
		if err := r.store.Update(ctx, b); err != nil {
			return true, err
		}
	}
	if !b.Attached {
		return true, nil // detached, counting down — do not self-heal
	}
	return false, nil
}

func (r *Reconciler) healIfDead(ctx context.Context, b *Box) error {
	if b.InstanceRef == "" {
		return r.provision(ctx, b)
	}
	inst, err := r.compute.Status(ctx, b.InstanceRef)
	if err != nil {
		return fmt.Errorf("status %s: %w", b.InstanceRef, err)
	}
	if inst.Phase == ports.InstanceGone || inst.Phase == ports.InstanceFailed {
		return r.provision(ctx, b)
	}
	return nil
}

func (r *Reconciler) checkAlive(ctx context.Context, b *Box) error {
	if b.InstanceRef == "" {
		return nil
	}
	inst, err := r.compute.Status(ctx, b.InstanceRef)
	if err != nil {
		return err
	}
	if inst.Phase == ports.InstanceFailed || inst.Phase == ports.InstanceGone {
		return r.fail(ctx, b, fmt.Errorf("instance %s phase=%s before agent connected", b.InstanceRef, inst.Phase))
	}
	return nil
}

func (r *Reconciler) destroy(ctx context.Context, b *Box) error {
	if r.cfg.Hooks != nil {
		// Best-effort (e.g. ingress unexpose); route cleanup must not block teardown.
		if err := r.cfg.Hooks.PreDestroy(ctx, b); err != nil {
			log.Printf("boxreconcile: pre-destroy %s (%s): %v", b.ID, b.Name, err)
		}
	}
	if b.InstanceRef != "" {
		if err := r.compute.Destroy(ctx, b.InstanceRef); err != nil {
			return err
		}
	}
	return r.store.Delete(ctx, b.TenantID, b.ID)
}

func (r *Reconciler) setPhase(ctx context.Context, b *Box, p Phase, msg string) error {
	if !CanTransition(b.Phase, p) {
		return fmt.Errorf("illegal transition %s->%s", b.Phase, p)
	}
	if p == PhaseRunning && b.LastActive.IsZero() {
		b.LastActive = r.now() // start the idle clock so a box that never heartbeats can still suspend
	}
	b.Phase = p
	b.Message = msg
	return r.store.Update(ctx, b)
}

func (r *Reconciler) fail(ctx context.Context, b *Box, cause error) error {
	b.Phase = PhaseFailed
	b.Message = cause.Error()
	_ = r.store.Update(ctx, b)
	return cause
}
