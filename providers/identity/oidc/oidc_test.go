package oidc

import (
	"context"
	"errors"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

type fakeVerifier struct {
	claims Claims
	err    error
}

func (f fakeVerifier) Verify(context.Context, string) (Claims, error) { return f.claims, f.err }

func TestAuthenticateMapsSubject(t *testing.T) {
	p := New(fakeVerifier{claims: Claims{Subject: "u-123", Email: "alice@corp.com"}}, Config{TenantID: "acme"})
	pr, err := p.Authenticate(context.Background(), ports.Credential{Scheme: "oidc-token", Value: "jwt"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != "u-123" || pr.TenantID != "acme" || pr.DisplayName != "alice@corp.com" {
		t.Fatalf("principal = %+v", pr)
	}
	if len(pr.Roles) != 1 || pr.Roles[0] != "owner" {
		t.Fatalf("roles = %v", pr.Roles)
	}
}

func TestAuthenticateEmailClaimAndAdminGroup(t *testing.T) {
	p := New(fakeVerifier{claims: Claims{Subject: "u-1", Email: "bob@corp.com", Groups: []string{"eng", "platform-admins"}}},
		Config{TenantID: "acme", PrincipalClaim: "email", AdminGroups: []string{"platform-admins"}})
	pr, err := p.Authenticate(context.Background(), ports.Credential{Value: "jwt"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.ID != "bob@corp.com" {
		t.Fatalf("id = %q, want email", pr.ID)
	}
	if !hasRole(pr.Roles, "tenant-admin") {
		t.Fatalf("expected tenant-admin from group membership, roles=%v", pr.Roles)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	p := New(fakeVerifier{err: errors.New("bad signature")}, Config{TenantID: "acme"})
	if _, err := p.Authenticate(context.Background(), ports.Credential{Value: "jwt"}); err == nil {
		t.Fatal("expected verify error to surface")
	}
	if _, err := p.Authenticate(context.Background(), ports.Credential{Value: ""}); err == nil {
		t.Fatal("expected empty-token error")
	}
	// missing the configured principal claim.
	p2 := New(fakeVerifier{claims: Claims{Email: "x@y"}}, Config{TenantID: "acme"}) // PrincipalClaim defaults to sub
	if _, err := p2.Authenticate(context.Background(), ports.Credential{Value: "jwt"}); err == nil {
		t.Fatal("expected missing-sub error")
	}
}

func hasRole(roles []string, r string) bool {
	for _, x := range roles {
		if x == r {
			return true
		}
	}
	return false
}
