package conformance

import (
	"context"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// RunIdentityConformance exercises the ports.Identity contract against a provider
// from factory. valid is a credential the provider MUST authenticate; invalid is
// one it MUST reject. Contract notes for provider authors:
//   - Authenticate(valid) MUST return a Principal with non-empty ID and TenantID.
//   - Authenticate(invalid) MUST return an error.
//   - Authorize MUST allow a principal holding the "system" role (system is
//     all-powerful in the coarse RBAC model) and MUST deny a principal with no
//     roles (with a non-empty reason).
func RunIdentityConformance(t *testing.T, factory func(t *testing.T) ports.Identity, valid, invalid ports.Credential) {
	t.Helper()

	t.Run("AuthenticateValidReturnsPrincipal", func(t *testing.T) {
		id := factory(t)
		p, err := id.Authenticate(context.Background(), valid)
		if err != nil {
			t.Fatalf("authenticate valid: %v", err)
		}
		if p.ID == "" || p.TenantID == "" {
			t.Fatalf("principal must carry id+tenant: %+v", p)
		}
	})

	t.Run("AuthenticateInvalidErrors", func(t *testing.T) {
		id := factory(t)
		if _, err := id.Authenticate(context.Background(), invalid); err == nil {
			t.Fatal("expected error authenticating an invalid credential")
		}
	})

	t.Run("AuthorizeSystemAllowed", func(t *testing.T) {
		id := factory(t)
		d, err := id.Authorize(context.Background(), ports.AccessRequest{
			Principal: ports.Principal{ID: "sys", TenantID: "default", Roles: []string{"system"}},
			Action:    "workspace.create", Resource: "default",
		})
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		if !d.Allow {
			t.Fatalf("system principal must be allowed: %+v", d)
		}
	})

	t.Run("AuthorizeNoRoleDenied", func(t *testing.T) {
		id := factory(t)
		d, err := id.Authorize(context.Background(), ports.AccessRequest{
			Principal: ports.Principal{ID: "nobody", TenantID: "default"},
			Action:    "workspace.create", Resource: "default",
		})
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		if d.Allow {
			t.Fatalf("role-less principal must be denied")
		}
		if d.Reason == "" {
			t.Fatalf("a deny must carry a reason")
		}
	})
}
