// Package oidc is the SSO Identity provider: it authenticates an OIDC ID token
// (issued by the org's IdP — Google, Okta, Entra, Keycloak…) to a Hopbox
// Principal. It is the enterprise counterpart to the static provider: identity,
// group→role mapping, and revocation live in the IdP, not in Hopbox. Token
// verification is behind a Verifier seam so the mapping logic is unit-testable
// without a live IdP.
package oidc

import (
	"context"
	"fmt"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Claims is the subset of an ID token Hopbox maps onto a Principal.
type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups"`
}

// Verifier validates a raw ID token (signature, issuer, audience, expiry) and
// returns its claims. The real implementation wraps go-oidc; tests fake it.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (Claims, error)
}

// Config controls how verified claims become a Principal.
type Config struct {
	TenantID       string   // tenant assigned to all OIDC users (single-tenant M-level)
	PrincipalClaim string   // "sub" (default) or "email" -> Principal.ID
	AdminGroups    []string // membership in any of these grants the tenant-admin role
}

// Provider implements ports.Identity over an OIDC Verifier.
type Provider struct {
	verifier Verifier
	cfg      Config
}

var _ ports.Identity = (*Provider)(nil)

func New(v Verifier, cfg Config) *Provider {
	if cfg.PrincipalClaim == "" {
		cfg.PrincipalClaim = "sub"
	}
	return &Provider{verifier: v, cfg: cfg}
}

func (p *Provider) Authenticate(ctx context.Context, c ports.Credential) (ports.Principal, error) {
	if c.Scheme != "" && c.Scheme != "oidc-token" && c.Scheme != "api-key" {
		return ports.Principal{}, fmt.Errorf("oidc: unsupported credential scheme %q", c.Scheme)
	}
	if c.Value == "" {
		return ports.Principal{}, fmt.Errorf("oidc: empty token")
	}
	claims, err := p.verifier.Verify(ctx, c.Value)
	if err != nil {
		return ports.Principal{}, fmt.Errorf("oidc: %w", err)
	}
	id := claims.Subject
	if p.cfg.PrincipalClaim == "email" {
		id = claims.Email
	}
	if id == "" {
		return ports.Principal{}, fmt.Errorf("oidc: token missing %q claim", p.cfg.PrincipalClaim)
	}
	roles := []string{"owner"}
	if groupsIntersect(claims.Groups, p.cfg.AdminGroups) {
		roles = append(roles, "tenant-admin")
	}
	return ports.Principal{ID: id, TenantID: p.cfg.TenantID, DisplayName: claims.Email, Roles: roles}, nil
}

// Authorize is the same coarse RBAC as the static provider: a privileged role
// allows everything; ownership scoping is enforced in the API layer.
func (p *Provider) Authorize(_ context.Context, r ports.AccessRequest) (ports.Decision, error) {
	for _, role := range r.Principal.Roles {
		if role == "system" || role == "tenant-admin" || role == "owner" {
			return ports.Decision{Allow: true}, nil
		}
	}
	return ports.Decision{Allow: false, Reason: "principal lacks a privileged role"}, nil
}

func groupsIntersect(have, want []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, g := range have {
		set[g] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; ok {
			return true
		}
	}
	return false
}

// NewVerifier builds a real go-oidc Verifier: it fetches the issuer's discovery
// document + JWKS (needs network at startup) and validates audience on Verify.
func NewVerifier(ctx context.Context, issuer, audience string) (Verifier, error) {
	prov, err := coreoidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover %s: %w", issuer, err)
	}
	cfg := &coreoidc.Config{ClientID: audience}
	if audience == "" {
		cfg.SkipClientIDCheck = true
	}
	return &liveVerifier{v: prov.Verifier(cfg)}, nil
}

type liveVerifier struct{ v *coreoidc.IDTokenVerifier }

func (l *liveVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	tok, err := l.v.Verify(ctx, raw)
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return Claims{}, err
	}
	return c, nil
}
