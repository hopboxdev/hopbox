package box

import (
	"context"
	"errors"
)

// ErrNotFound is returned by a Store when no box matches. The dev-env adapter
// maps its store's not-found to this so box-core stays dependency-free.
var ErrNotFound = errors.New("box: not found")

// Store is the box persistence the Engine needs — a box-view of whatever backs
// it. The dev-env adapts its workspace store to this interface.
type Store interface {
	Get(ctx context.Context, tenant, id string) (*Box, error)
	GetByName(ctx context.Context, tenant, name string) (*Box, error)
	List(ctx context.Context, tenant string) ([]*Box, error)
	Create(ctx context.Context, b *Box) error
	Update(ctx context.Context, b *Box) error
	Delete(ctx context.Context, tenant, id string) error
}
