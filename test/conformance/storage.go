// Package conformance holds shared provider contract test batteries. Any
// provider of a contract must pass its battery — including third-party bricks.
package conformance

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// RunStorageConformance exercises the ports.Storage contract against a provider
// produced by factory.
func RunStorageConformance(t *testing.T, factory func(t *testing.T) ports.Storage) {
	t.Helper()

	t.Run("EnsureHomeReturnsMount", func(t *testing.T) {
		s := factory(t)
		m, err := s.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1", TenantID: "default", Owner: "alice"})
		if err != nil {
			t.Fatalf("ensurehome: %v", err)
		}
		if m.Source == "" || m.Target == "" {
			t.Fatalf("mount must have source+target: %+v", m)
		}
	})

	t.Run("EnsureHomeIdempotent", func(t *testing.T) {
		s := factory(t)
		ctx := context.Background()
		m1, err := s.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"})
		if err != nil {
			t.Fatal(err)
		}
		m2, err := s.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"})
		if err != nil {
			t.Fatalf("second ensure: %v", err)
		}
		if m1.Source != m2.Source {
			t.Fatalf("idempotent ensure changed source: %q -> %q", m1.Source, m2.Source)
		}
	})

	t.Run("DeleteRemoves", func(t *testing.T) {
		s := factory(t)
		ctx := context.Background()
		m, err := s.EnsureHome(ctx, ports.HomeRequest{WorkspaceID: "w1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, m.Source); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})
}
