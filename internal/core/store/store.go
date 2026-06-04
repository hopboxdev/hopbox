// Package store is the StateStore seam. M1 ships a SQLite impl; Postgres is M5+.
package store

import (
	"context"
	"errors"

	"github.com/mesadev/mesa/internal/core/workspace"
)

var ErrNotFound = errors.New("store: not found")

type Store interface {
	CreateWorkspace(ctx context.Context, w *workspace.Workspace) error
	GetWorkspace(ctx context.Context, tenantID, id string) (*workspace.Workspace, error)
	GetByName(ctx context.Context, tenantID, name string) (*workspace.Workspace, error)
	GetByToken(ctx context.Context, token string) (*workspace.Workspace, error)
	ListWorkspaces(ctx context.Context, tenantID string) ([]*workspace.Workspace, error)
	ListAll(ctx context.Context) ([]*workspace.Workspace, error) // reconciler scan
	UpdateWorkspace(ctx context.Context, w *workspace.Workspace) error
	DeleteWorkspace(ctx context.Context, tenantID, id string) error
	Close() error
}
