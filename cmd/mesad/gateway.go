package main

import (
	"context"
	"net"

	"github.com/mesadev/mesa/internal/agenthub"
	"github.com/mesadev/mesa/internal/gateway"
	"github.com/mesadev/mesa/providers/ingress/subdomain"
)

// subdomainRouter adapts the subdomain Ingress provider's route table to the
// gateway.Router interface.
type subdomainRouter struct{ p *subdomain.Provider }

func (r subdomainRouter) Lookup(host string) (string, int32, bool) {
	rt, ok := r.p.Lookup(host)
	return rt.WorkspaceID, rt.Port, ok
}

// hubDialer adapts the agent hub to gateway.Dialer: each DialWorkspace opens a
// fresh forward stream into the workspace over its agent reverse-connection.
type hubDialer struct{ h *agenthub.Hub }

func (d hubDialer) DialWorkspace(_ context.Context, workspaceID string, port int32) (net.Conn, error) {
	return d.h.OpenForward(workspaceID, uint32(port))
}

func newGateway(ig *subdomain.Provider, hub *agenthub.Hub) *gateway.Gateway {
	return gateway.New(subdomainRouter{ig}, hubDialer{hub})
}
