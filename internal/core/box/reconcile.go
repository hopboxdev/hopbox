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
	Interval  time.Duration    // backstop sweep period (default 1s)
	Now       func() time.Time // clock seam; nil = time.Now
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
		if !b.AgentConnected {
			return r.healIfDead(ctx, b)
		}
		return nil
	case PhaseDestroying:
		return r.destroy(ctx, b)
	default:
		return nil // Failed/Stopped terminal
	}
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
	inst, err := r.compute.Provision(ctx, ports.ProvisionRequest{
		WorkspaceID: b.ID,
		ImageRef:    b.ImageRef,
		MemMB:       b.MemMB,
		CPUMillis:   b.CPUMillis,
		Agent:       r.cfg.Agent,
		Env:         env,
	})
	if err != nil {
		return r.fail(ctx, b, fmt.Errorf("compute: %w", err))
	}
	b.InstanceRef = inst.Ref
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
