// Package boxstore adapts the dev-env workspace store to box.Store — a box-view
// of the shared persistence. It lives in the dev-env layer (it knows both box
// and workspace) so box-core stays dependency-free: the box Engine depends only
// on box.Store, and the daemon injects this adapter.
package boxstore

import (
	"context"
	"errors"

	"github.com/hopboxdev/hopbox/internal/core/box"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

// Adapter wraps a workspace store and presents it as a box.Store.
type Adapter struct{ s store.Store }

var _ box.Store = (*Adapter)(nil)

func New(s store.Store) *Adapter { return &Adapter{s: s} }

func mapErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return box.ErrNotFound
	}
	return err
}

func (a *Adapter) Get(ctx context.Context, tenant, id string) (*box.Box, error) {
	w, err := a.s.GetWorkspace(ctx, tenant, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return &w.Box, nil
}

func (a *Adapter) GetByName(ctx context.Context, tenant, name string) (*box.Box, error) {
	w, err := a.s.GetByName(ctx, tenant, name)
	if err != nil {
		return nil, mapErr(err)
	}
	return &w.Box, nil
}

func (a *Adapter) List(ctx context.Context, tenant string) ([]*box.Box, error) {
	// box.Reconciler.Run sweeps with tenant "" meaning "all"; the workspace store
	// filters tenant_id='' to nothing, so map the empty tenant onto ListAll.
	list := a.s.ListWorkspaces
	if tenant == "" {
		list = func(ctx context.Context, _ string) ([]*workspace.Workspace, error) { return a.s.ListAll(ctx) }
	}
	ws, err := list(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := make([]*box.Box, len(ws))
	for i, w := range ws {
		out[i] = &w.Box
	}
	return out, nil
}

func (a *Adapter) Create(ctx context.Context, b *box.Box) error {
	return a.s.CreateWorkspace(ctx, &workspace.Workspace{Box: *b})
}

// Update is read-modify-write so it preserves any dev-env decorations (a
// persistent home, gateway endpoints) on a workspace that also has a box-view.
func (a *Adapter) Update(ctx context.Context, b *box.Box) error {
	w, err := a.s.GetWorkspace(ctx, b.TenantID, b.ID)
	if err != nil {
		return mapErr(err)
	}
	w.Box = *b
	return a.s.UpdateWorkspace(ctx, w)
}

func (a *Adapter) Delete(ctx context.Context, tenant, id string) error {
	return a.s.DeleteWorkspace(ctx, tenant, id)
}
