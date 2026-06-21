// Package gateway is mesa-gw: the stateless service gateway. It terminates
// inbound HTTP, resolves the request's Host to a workspace + port via the
// Router, and reverse-proxies the request INTO that workspace over a Dialer —
// in production a fresh agent forward stream (mesad's hub.OpenForward). It needs
// no route into compute: the workspace dialed out, the gateway rides that conn.
package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
)

// Router resolves an inbound gateway Host to a workspace id + target port.
type Router interface {
	Lookup(host string) (workspaceID string, port int32, ok bool)
}

// Dialer opens a connection to a port inside a workspace. The returned conn is a
// raw byte pipe to that service (the agent forward stream in production).
type Dialer interface {
	DialWorkspace(ctx context.Context, workspaceID string, port int32) (net.Conn, error)
}

// Gateway is an http.Handler that proxies each request into the workspace its
// Host maps to.
type Gateway struct {
	router Router
	dialer Dialer
}

func New(router Router, dialer Dialer) *Gateway {
	return &Gateway{router: router, dialer: dialer}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	wsID, port, ok := g.router.Lookup(host)
	if !ok {
		http.Error(w, "mesa-gw: no route for host "+host, http.StatusNotFound)
		return
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// The Transport below ignores the address; set a syntactically valid
			// target so ReverseProxy builds the outbound request.
			req.URL.Scheme = "http"
			req.URL.Host = host
		},
		Transport: &http.Transport{
			// Every request rides a fresh forward stream into the workspace,
			// regardless of the dialed address.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return g.dialer.DialWorkspace(ctx, wsID, port)
			},
			DisableKeepAlives: true,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "mesa-gw: upstream error: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}
