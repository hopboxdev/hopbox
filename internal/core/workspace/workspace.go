package workspace

import "github.com/hopboxdev/hopbox/internal/core/box"

// IngressPort is a desired exposed endpoint: a named port inside the workspace.
type IngressPort struct {
	Name string `json:"name"` // logical endpoint name (e.g. "app")
	Port int32  `json:"port"` // port the workspace service listens on
}

// Endpoint is an observed exposed endpoint, written by the reconciler after the
// Ingress provider resolves an IngressPort to a reachable address.
type Endpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`  // reachable address, e.g. https://app-w1.gw.host
	Port int32  `json:"port"` // the workspace port it targets
	Ref  string `json:"ref"`  // provider-opaque handle / gateway route key
}

// Workspace is the dev-env resource: a box.Box plus the dev-environment
// decorations — a persistent home and gateway-exposed endpoints. It embeds
// box.Box, so every box field and method (Phase, lifetime, EvalLifetime, …)
// promotes through and existing call sites keep working.
type Workspace struct {
	box.Box
	Ingress   []IngressPort // desired exposed endpoints (gateway)
	HomeMount string        // host path of the persistent home (storage)
	Endpoints []Endpoint    // resolved endpoints (one per Ingress, set when Running)
}

// New returns a Pending Workspace wrapping a fresh box.
func New(tenantID, owner, name, imageRef string) *Workspace {
	return &Workspace{Box: *box.New(tenantID, owner, name, imageRef)}
}
