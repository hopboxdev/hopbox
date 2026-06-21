package static

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

func newProv() *Provider {
	return New(map[string]ports.Principal{
		"k-owner": {ID: "alice", TenantID: "default", Roles: []string{"owner"}},
		"k-plain": {ID: "bob", TenantID: "default"},
	})
}

func TestAuthenticate(t *testing.T) {
	p := newProv()
	pr, err := p.Authenticate(context.Background(), ports.Credential{Scheme: "api-key", Value: "k-owner"})
	if err != nil || pr.ID != "alice" {
		t.Fatalf("authenticate owner: %+v err=%v", pr, err)
	}
	if _, err := p.Authenticate(context.Background(), ports.Credential{Value: "bogus"}); err == nil {
		t.Fatal("expected error for unknown key")
	}
	if _, err := p.Authenticate(context.Background(), ports.Credential{Scheme: "oidc-token", Value: "x"}); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestAuthorizeCoarseRBAC(t *testing.T) {
	p := newProv()
	owner, _ := p.Authenticate(context.Background(), ports.Credential{Value: "k-owner"})
	if d, _ := p.Authorize(context.Background(), ports.AccessRequest{Principal: owner, Action: "workspace.create"}); !d.Allow {
		t.Fatal("owner must be allowed")
	}
	plain, _ := p.Authenticate(context.Background(), ports.Credential{Value: "k-plain"})
	if d, _ := p.Authorize(context.Background(), ports.AccessRequest{Principal: plain, Action: "workspace.create"}); d.Allow {
		t.Fatal("role-less principal must be denied")
	}
}
