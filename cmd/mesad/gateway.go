package main

import (
	"context"
	"net"

	"github.com/mesadev/mesa/internal/agenthub"
	"github.com/mesadev/mesa/internal/gateway"
	"github.com/mesadev/mesa/providers/ingress/subdomain"
)

// inProcConnector resolves a gateway Host against the subdomain route table and
// opens a forward stream into the workspace via the agent hub — the in-process
// path used by mesad's embedded gateway and served to remote mesa-gw over the
// tunnel.
type inProcConnector struct {
	ig  *subdomain.Provider
	hub *agenthub.Hub
}

var _ gateway.Connector = inProcConnector{}

func (c inProcConnector) Connect(_ context.Context, host string) (net.Conn, error) {
	rt, ok := c.ig.Lookup(host)
	if !ok {
		return nil, gateway.ErrNoRoute
	}
	return c.hub.OpenForward(rt.WorkspaceID, uint32(rt.Port))
}

func newConnector(ig *subdomain.Provider, hub *agenthub.Hub) inProcConnector {
	return inProcConnector{ig: ig, hub: hub}
}

func newGateway(ig *subdomain.Provider, hub *agenthub.Hub) *gateway.Gateway {
	return gateway.New(newConnector(ig, hub))
}

func newTunnelServer(ig *subdomain.Provider, hub *agenthub.Hub) *gateway.TunnelServer {
	return gateway.NewTunnelServer(newConnector(ig, hub))
}
