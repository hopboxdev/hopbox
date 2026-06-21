package conformance

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

// RunMeteringConformance exercises the ports.Metering contract against a provider
// from factory. Contract notes for provider authors:
//   - Emit MUST accept a well-formed usage event without error.
//   - Quota MUST return a well-formed QuotaState; a denial (Allowed=false) MUST
//     carry a non-empty Reason, and WorkspacesLimit MUST be >= 0.
//
// The battery is provider-agnostic: it does not assume Emit affects Quota (a
// metrics-only provider like prometheus may not enforce limits). Provider-
// specific enforcement is covered by that provider's own unit tests.
func RunMeteringConformance(t *testing.T, factory func(t *testing.T) ports.Metering) {
	t.Helper()

	t.Run("EmitAccepted", func(t *testing.T) {
		m := factory(t)
		err := m.Emit(context.Background(), ports.UsageEvent{
			TenantID: "default", PrincipalID: "alice", WorkspaceID: "w1",
			Kind: "workspace.start", Value: 1, UnixMillis: 1,
		})
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
	})

	t.Run("QuotaWellFormed", func(t *testing.T) {
		m := factory(t)
		q, err := m.Quota(context.Background(), ports.PrincipalRef{PrincipalID: "alice", TenantID: "default"})
		if err != nil {
			t.Fatalf("quota: %v", err)
		}
		if q.WorkspacesLimit < 0 {
			t.Fatalf("limit must be >= 0: %+v", q)
		}
		if !q.Allowed && q.Reason == "" {
			t.Fatalf("a denial must carry a reason: %+v", q)
		}
	})
}
