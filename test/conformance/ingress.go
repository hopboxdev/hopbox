package conformance

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// RunIngressConformance exercises the ports.Ingress contract against a provider
// from factory. Contract notes for provider authors:
//   - Expose MUST return an Endpoint with a non-empty Ref and URL.
//   - Expose MUST be idempotent for the same (workspace_id, name): re-exposing
//     returns the same Ref (the gateway route key is stable).
//   - Unexpose MUST be idempotent (unexposing an unknown ref is not an error).
func RunIngressConformance(t *testing.T, factory func(t *testing.T) ports.Ingress) {
	t.Helper()

	req := ports.ExposeRequest{WorkspaceID: "w1", Name: "app", Port: 3000, Scheme: "subdomain", TenantID: "default"}

	t.Run("ExposeReturnsEndpoint", func(t *testing.T) {
		ig := factory(t)
		ep, err := ig.Expose(context.Background(), req)
		if err != nil {
			t.Fatalf("expose: %v", err)
		}
		if ep.Ref == "" || ep.URL == "" {
			t.Fatalf("endpoint must have ref+url: %+v", ep)
		}
		if ep.Port != req.Port {
			t.Fatalf("endpoint port = %d, want %d", ep.Port, req.Port)
		}
	})

	t.Run("ExposeIsIdempotent", func(t *testing.T) {
		ig := factory(t)
		ctx := context.Background()
		e1, err := ig.Expose(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		e2, err := ig.Expose(ctx, req)
		if err != nil {
			t.Fatalf("second expose: %v", err)
		}
		if e1.Ref != e2.Ref {
			t.Fatalf("idempotent expose changed ref: %q -> %q", e1.Ref, e2.Ref)
		}
	})

	t.Run("UnexposeRemovesAndIsIdempotent", func(t *testing.T) {
		ig := factory(t)
		ctx := context.Background()
		ep, err := ig.Expose(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if err := ig.Unexpose(ctx, ep.Ref); err != nil {
			t.Fatalf("unexpose: %v", err)
		}
		if err := ig.Unexpose(ctx, ep.Ref); err != nil {
			t.Fatalf("second unexpose (must be idempotent): %v", err)
		}
	})
}
