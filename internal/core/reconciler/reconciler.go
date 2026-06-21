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
	AgentAddr string           // what the agent dials, e.g. host.docker.internal:7777
	Agent     ports.AgentImage // how to side-load the agent into a workspace
	Interval  time.Duration
}

type Reconciler struct {
	store   store.Store
	compute ports.Compute
	storage ports.Storage
	ingress ports.Ingress // optional; nil disables endpoint reconciliation
	cfg     Config
}

func New(s store.Store, c ports.Compute, st ports.Storage, ig ports.Ingress, cfg Config) *Reconciler {
	if cfg.Interval == 0 {
		cfg.Interval = time.Second
	}
	return &Reconciler{store: s, compute: c, storage: st, ingress: ig, cfg: cfg}
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
			return r.healIfInstanceDead(ctx, w)
		}
		return r.reconcileIngress(ctx, w)
	case workspace.PhaseDestroying:
		return r.destroy(ctx, w)
	default:
		// Failed and Stopped are terminal in M1: no auto-retry/backoff loop yet
		// (Failed->Provisioning is a legal edge but intentionally not driven here;
		// recovery is `mesa rm` + recreate). Revisit with bounded retry post-M1.
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
		Agent:       r.cfg.Agent,
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
	if !workspace.CanTransition(w.Phase, workspace.PhaseProvisioning) {
		return r.fail(ctx, w, fmt.Errorf("illegal transition %s->Provisioning", w.Phase))
	}
	w.Phase = workspace.PhaseProvisioning
	w.Message = "provisioned, awaiting agent"
	return r.store.UpdateWorkspace(ctx, w)
}

// healIfInstanceDead handles a Running workspace reporting no agent. The agent
// may merely be mid-reconnect (a transient yamux blip), so we re-provision ONLY
// if the compute instance is actually gone/failed. If the container is still
// alive, we leave the workspace Running and let the agent redial — this avoids
// destroying a live workspace on every network hiccup.
func (r *Reconciler) healIfInstanceDead(ctx context.Context, w *workspace.Workspace) error {
	if w.InstanceRef == "" {
		return r.provision(ctx, w) // never provisioned a box; do it now
	}
	inst, err := r.compute.Status(ctx, w.InstanceRef)
	if err != nil {
		return fmt.Errorf("status %s: %w", w.InstanceRef, err) // transient; retry next tick
	}
	if inst.Phase == ports.InstanceGone || inst.Phase == ports.InstanceFailed {
		// container really died: self-heal by re-provisioning.
		return r.provision(ctx, w)
	}
	// container still alive; agent is mid-reconnect. Leave it.
	return nil
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

// reconcileIngress exposes each desired IngressPort that lacks a resolved
// Endpoint, via the Ingress provider, and persists the new endpoints. Expose is
// idempotent, so a missed persist just re-resolves to the same address next tick.
func (r *Reconciler) reconcileIngress(ctx context.Context, w *workspace.Workspace) error {
	if r.ingress == nil || len(w.Ingress) == 0 {
		return nil
	}
	have := make(map[string]bool, len(w.Endpoints))
	for _, e := range w.Endpoints {
		have[e.Name] = true
	}
	changed := false
	for _, ip := range w.Ingress {
		if have[ip.Name] {
			continue
		}
		ep, err := r.ingress.Expose(ctx, ports.ExposeRequest{
			WorkspaceID: w.ID, Name: ip.Name, Port: ip.Port, Scheme: "subdomain", TenantID: w.TenantID,
		})
		if err != nil {
			return fmt.Errorf("ingress expose %q: %w", ip.Name, err)
		}
		w.Endpoints = append(w.Endpoints, workspace.Endpoint{Name: ep.Name, URL: ep.URL, Port: ep.Port, Ref: ep.Ref})
		changed = true
	}
	if changed {
		return r.store.UpdateWorkspace(ctx, w)
	}
	return nil
}

func (r *Reconciler) destroy(ctx context.Context, w *workspace.Workspace) error {
	if r.ingress != nil {
		for _, e := range w.Endpoints {
			_ = r.ingress.Unexpose(ctx, e.Ref) // best-effort; route removal must not block teardown
		}
	}
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
