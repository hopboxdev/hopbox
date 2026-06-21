// Package static is the config-defined Identity provider: a fixed set of API
// keys, each mapping to a Principal. Authorization is coarse RBAC (owner |
// tenant-admin | system) per the design. It is the zero-dependency default for
// solo/self-hosted deploys; oidc is the post-MVP SSO provider.
package static

import (
	"context"
	"fmt"

	"github.com/mesadev/mesa/internal/core/ports"
)

// Provider authenticates api-key credentials against a fixed key→Principal map.
type Provider struct {
	keys map[string]ports.Principal // api-key value -> principal
}

var _ ports.Identity = (*Provider)(nil)

// New builds a static Identity provider. keys maps an api-key string to the
// Principal it authenticates as. The map is copied; callers may mutate theirs.
func New(keys map[string]ports.Principal) *Provider {
	cp := make(map[string]ports.Principal, len(keys))
	for k, v := range keys {
		cp[k] = v
	}
	return &Provider{keys: cp}
}

// privileged roles can do anything in the coarse MVP RBAC model.
func privileged(role string) bool {
	return role == "system" || role == "tenant-admin" || role == "owner"
}

func (p *Provider) Authenticate(_ context.Context, c ports.Credential) (ports.Principal, error) {
	if c.Scheme != "" && c.Scheme != "api-key" {
		return ports.Principal{}, fmt.Errorf("static: unsupported credential scheme %q (want api-key)", c.Scheme)
	}
	if c.Value == "" {
		return ports.Principal{}, fmt.Errorf("static: empty credential")
	}
	pr, ok := p.keys[c.Value]
	if !ok {
		return ports.Principal{}, fmt.Errorf("static: unknown api key")
	}
	return pr, nil
}

// Authorize is coarse RBAC: a principal holding system/tenant-admin/owner is
// allowed; anyone else is denied. (Fine-grained, action-aware RBAC is post-MVP.)
func (p *Provider) Authorize(_ context.Context, r ports.AccessRequest) (ports.Decision, error) {
	for _, role := range r.Principal.Roles {
		if privileged(role) {
			return ports.Decision{Allow: true}, nil
		}
	}
	return ports.Decision{Allow: false, Reason: "principal lacks a privileged role (owner|tenant-admin|system)"}, nil
}
