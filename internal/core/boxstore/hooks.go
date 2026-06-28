package boxstore

import (
	"context"
	"fmt"
	"path"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// Hooks implements box.Hooks for the dev-env: it folds the workspace-level
// concerns the box reconciler doesn't know about — a persistent storage home and
// gateway ingress — onto box.Reconciler. It lives in the dev-env layer (knows
// workspace + storage + ingress) so box-core stays dependency-free.
type Hooks struct {
	store   store.Store
	storage ports.Storage
	ingress ports.Ingress // optional; nil disables endpoint reconciliation
	cfg     HooksConfig
}

// HooksConfig carries the dev-env's in-box SSH settings injected at provision.
type HooksConfig struct {
	TrustedUserCA  string // CA the box's sshd trusts (HOPBOX_TRUSTED_USER_CA)
	AuthorizedKeys string // keys injected into the box (HOPBOX_AUTHORIZED_KEYS)
}

var _ box.Hooks = (*Hooks)(nil)

func NewHooks(s store.Store, storage ports.Storage, ingress ports.Ingress, cfg HooksConfig) *Hooks {
	return &Hooks{store: s, storage: storage, ingress: ingress, cfg: cfg}
}

// PreProvision ensures the workspace's persistent home and returns it as a mount
// plus the dev-env env (the SSH host key on the home, trusted CA, authorized
// keys). It records the home source on the workspace.
func (h *Hooks) PreProvision(ctx context.Context, b *box.Box) ([]ports.Mount, map[string]string, error) {
	mount, err := h.storage.EnsureHome(ctx, ports.HomeRequest{
		WorkspaceID: b.ID, TenantID: b.TenantID, Owner: b.Owner,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: %w", err)
	}
	w, err := h.store.GetWorkspace(ctx, b.TenantID, b.ID)
	if err != nil {
		return nil, nil, err
	}
	w.HomeMount = mount.Source
	if err := h.store.UpdateWorkspace(ctx, w); err != nil {
		return nil, nil, err
	}
	env := map[string]string{
		// persist the SSH host key on the home volume so known_hosts pinning
		// survives restarts.
		"HOPBOX_SSH_HOST_KEY": path.Join(mount.Target, ".hopbox", "ssh_host_ed25519_key"),
	}
	if h.cfg.TrustedUserCA != "" {
		env["HOPBOX_TRUSTED_USER_CA"] = h.cfg.TrustedUserCA
	}
	if h.cfg.AuthorizedKeys != "" {
		env["HOPBOX_AUTHORIZED_KEYS"] = h.cfg.AuthorizedKeys
	}
	return []ports.Mount{mount}, env, nil
}

// PostRunning exposes each desired ingress port that lacks a resolved endpoint
// and persists the new endpoints. Expose is idempotent, so a missed persist just
// re-resolves to the same address next tick.
func (h *Hooks) PostRunning(ctx context.Context, b *box.Box) error {
	if h.ingress == nil {
		return nil
	}
	w, err := h.store.GetWorkspace(ctx, b.TenantID, b.ID)
	if err != nil {
		return err
	}
	if len(w.Ingress) == 0 {
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
		ep, err := h.ingress.Expose(ctx, ports.ExposeRequest{
			WorkspaceID: w.ID, Name: ip.Name, Port: ip.Port, Scheme: "subdomain", TenantID: w.TenantID,
		})
		if err != nil {
			return fmt.Errorf("ingress expose %q: %w", ip.Name, err)
		}
		w.Endpoints = append(w.Endpoints, workspace.Endpoint{Name: ep.Name, URL: ep.URL, Port: ep.Port, Ref: ep.Ref})
		changed = true
	}
	if changed {
		return h.store.UpdateWorkspace(ctx, w)
	}
	return nil
}

// PreDestroy unexposes the workspace's ingress endpoints. Best-effort: route
// removal must not block teardown.
func (h *Hooks) PreDestroy(ctx context.Context, b *box.Box) error {
	if h.ingress == nil {
		return nil
	}
	w, err := h.store.GetWorkspace(ctx, b.TenantID, b.ID)
	if err != nil {
		return nil // already gone
	}
	for _, e := range w.Endpoints {
		_ = h.ingress.Unexpose(ctx, e.Ref)
	}
	return nil
}
