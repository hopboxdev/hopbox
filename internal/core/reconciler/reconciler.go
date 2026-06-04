// Package reconciler drives each Workspace from its observed status toward its
// desired spec by calling providers. It is the Kubernetes-controller *pattern*
// with no Kubernetes dependency: observe -> diff -> act -> persist. Idempotent
// and crash-safe (state lives in the store).
package reconciler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/core/store"
	"github.com/mesadev/mesa/internal/core/workspace"
)

type Config struct {
	AgentAddr string // what the agent dials, e.g. host.docker.internal:7777
	AgentPath string // host path of the agent binary to side-load
	Interval  time.Duration
}

type Reconciler struct {
	store   store.Store
	compute ports.Compute
	storage ports.Storage
	cfg     Config
}

func New(s store.Store, c ports.Compute, st ports.Storage, cfg Config) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = time.Second
	}
	return &Reconciler{store: s, compute: c, storage: st, cfg: cfg}
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Run scans all workspaces on an interval until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			all, err := r.store.ListAll(ctx)
			if err != nil {
				log.Printf("reconciler: list: %v", err)
				continue
			}
			for _, w := range all {
				if err := r.ReconcileOne(ctx, w.ID, w.TenantID); err != nil {
					log.Printf("reconciler: workspace %s: %v", w.ID, err)
				}
			}
		}
	}
}

// ReconcileOne advances a single workspace one step toward its desired state.
func (r *Reconciler) ReconcileOne(ctx context.Context, id, tenantID string) error {
	w, err := r.store.GetWorkspace(ctx, tenantID, id)
	if err != nil {
		return err
	}
	switch w.Phase {
	case workspace.PhasePending:
		return r.provision(ctx, w)
	case workspace.PhaseProvisioning:
		if w.AgentConnected {
			return r.setPhase(ctx, w, workspace.PhaseRunning, "agent connected")
		}
		return r.checkComputeAlive(ctx, w)
	case workspace.PhaseRunning:
		if !w.AgentConnected {
			// agent gone: re-provision (self-heal).
			return r.provision(ctx, w)
		}
		return nil
	case workspace.PhaseDestroying:
		return r.destroy(ctx, w)
	default:
		return nil
	}
}

func (r *Reconciler) provision(ctx context.Context, w *workspace.Workspace) error {
	mount, err := r.storage.EnsureHome(ctx, ports.HomeRequest{
		WorkspaceID: w.ID, TenantID: w.TenantID, Owner: w.Owner,
	})
	if err != nil {
		return r.fail(ctx, w, fmt.Errorf("storage: %w", err))
	}
	if w.BootstrapToken == "" {
		w.BootstrapToken = newToken()
	}
	inst, err := r.compute.Provision(ctx, ports.ProvisionRequest{
		WorkspaceID: w.ID,
		ImageRef:    w.ImageRef,
		MemMB:       w.MemMB,
		Mounts:      []ports.Mount{mount},
		AgentPath:   r.cfg.AgentPath,
		Env: map[string]string{
			"MESA_AGENT_TOKEN":  w.BootstrapToken,
			"MESA_CONTROL_ADDR": r.cfg.AgentAddr,
			"MESA_WORKSPACE_ID": w.ID,
		},
	})
	if err != nil {
		return r.fail(ctx, w, fmt.Errorf("compute: %w", err))
	}
	w.HomeMount = mount.Source
	w.InstanceRef = inst.Ref
	w.AgentConnected = false
	w.Phase = workspace.PhaseProvisioning
	w.Message = "provisioned, awaiting agent"
	return r.store.UpdateWorkspace(ctx, w)
}

func (r *Reconciler) checkComputeAlive(ctx context.Context, w *workspace.Workspace) error {
	if w.InstanceRef == "" {
		return nil
	}
	inst, err := r.compute.Status(ctx, w.InstanceRef)
	if err != nil {
		return err
	}
	if inst.Phase == ports.InstanceFailed || inst.Phase == ports.InstanceGone {
		return r.fail(ctx, w, fmt.Errorf("instance %s phase=%s before agent connected", w.InstanceRef, inst.Phase))
	}
	return nil
}

func (r *Reconciler) destroy(ctx context.Context, w *workspace.Workspace) error {
	if w.InstanceRef != "" {
		if err := r.compute.Destroy(ctx, w.InstanceRef); err != nil {
			return err
		}
	}
	// M1: keep the home (persistence). Storage.Delete is wired for `mesa rm --purge` later.
	return r.store.DeleteWorkspace(ctx, w.TenantID, w.ID)
}

func (r *Reconciler) setPhase(ctx context.Context, w *workspace.Workspace, p workspace.Phase, msg string) error {
	if !workspace.CanTransition(w.Phase, p) {
		return fmt.Errorf("illegal transition %s->%s", w.Phase, p)
	}
	w.Phase = p
	w.Message = msg
	return r.store.UpdateWorkspace(ctx, w)
}

func (r *Reconciler) fail(ctx context.Context, w *workspace.Workspace, cause error) error {
	w.Phase = workspace.PhaseFailed
	w.Message = cause.Error()
	_ = r.store.UpdateWorkspace(ctx, w)
	return cause
}
