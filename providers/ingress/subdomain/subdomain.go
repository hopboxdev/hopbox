// Package subdomain is the MVP Ingress provider: it maps a workspace port onto
// an L7 Host-routed URL (app-alice.gw.host) on the shared gateway :443. Because
// routing is by Host header/SNI, there is no port allocator and the number of
// endpoints per user is unlimited. The provider owns a route table that hopbox-gw
// consults (Lookup) to forward an inbound request into the right workspace over
// that workspace's agent reverse-connection.
package subdomain

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Route is a resolved gateway entry: which workspace/port a Host maps to.
type Route struct {
	Host        string
	WorkspaceID string
	Port        int32
	Name        string
}

// Provider is an in-memory route table keyed by gateway Host. Safe for
// concurrent use. The zone is the wildcard DNS domain the gateway serves
// (e.g. "gw.example.com"); a wildcard TLS cert *.zone terminates all of them.
type Provider struct {
	zone   string
	mu     sync.RWMutex
	byHost map[string]Route
}

var _ ports.Ingress = (*Provider)(nil)

// New builds a subdomain Ingress provider serving *.zone.
func New(zone string) *Provider {
	return &Provider{zone: strings.TrimPrefix(zone, "."), byHost: map[string]Route{}}
}

// host builds the stable gateway host for an endpoint: <name>-<workspace>.<zone>.
// It is deterministic, so re-Exposing the same (workspace,name) is idempotent.
func (p *Provider) host(workspaceID, name string) string {
	return name + "-" + workspaceID + "." + p.zone
}

func (p *Provider) Expose(_ context.Context, r ports.ExposeRequest) (ports.Endpoint, error) {
	if r.WorkspaceID == "" || r.Name == "" {
		return ports.Endpoint{}, fmt.Errorf("subdomain: workspace_id and name are required")
	}
	if r.Port <= 0 {
		return ports.Endpoint{}, fmt.Errorf("subdomain: port must be > 0, got %d", r.Port)
	}
	host := p.host(r.WorkspaceID, r.Name)
	p.mu.Lock()
	p.byHost[host] = Route{Host: host, WorkspaceID: r.WorkspaceID, Port: r.Port, Name: r.Name}
	p.mu.Unlock()
	return ports.Endpoint{Ref: host, URL: "https://" + host, Name: r.Name, Port: r.Port}, nil
}

// Unexpose removes the route. ref is the Endpoint.Ref (the gateway host).
// Removing an unknown route is not an error (idempotent).
func (p *Provider) Unexpose(_ context.Context, ref string) error {
	p.mu.Lock()
	delete(p.byHost, ref)
	p.mu.Unlock()
	return nil
}

// Lookup resolves an inbound gateway Host to its Route. hopbox-gw calls this to
// forward a request into the right workspace. ok is false if no route exists.
func (p *Provider) Lookup(host string) (Route, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	rt, ok := p.byHost[host]
	return rt, ok
}
