// Package gateway is hopbox-gw: the service gateway. It terminates inbound HTTP,
// and for each request opens a connection to the workspace service its Host maps
// to (via a Connector) and reverse-proxies the request over that conn. The
// Connector hides whether the workspace is reached in-process (hopboxd's hub) or
// across a tunnel to a central hopboxd (the standalone hopbox-gw fleet). Either way
// the gateway needs no route into compute: the workspace dialed out.
package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
)

// ErrNoRoute means no workspace is mapped to the request Host (=> 404). Any
// other Connect error is an upstream/dial failure (=> 502).
var ErrNoRoute = errors.New("gateway: no route for host")

// Connector opens a raw byte pipe to the workspace service that serves a given
// gateway Host. The gateway proxies the HTTP request/response over the returned
// conn (in production a fresh agent forward stream, possibly across a tunnel).
type Connector interface {
	Connect(ctx context.Context, host string) (net.Conn, error)
}

// Gateway is an http.Handler that proxies each request into the workspace its
// Host maps to.
type Gateway struct{ conn Connector }

func New(c Connector) *Gateway { return &Gateway{conn: c} }

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	upstream, err := g.conn.Connect(r.Context(), host)
	if errors.Is(err, ErrNoRoute) {
		http.Error(w, "hopbox-gw: no route for host "+host, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "hopbox-gw: connect: "+err.Error(), http.StatusBadGateway)
		return
	}

	used := false
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// The Transport returns the already-open upstream conn; set a valid
			// target so ReverseProxy builds the outbound request.
			req.URL.Scheme = "http"
			req.URL.Host = host
		},
		Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				if used {
					return nil, errors.New("hopbox-gw: upstream conn already consumed")
				}
				used = true
				return upstream, nil
			},
			DisableKeepAlives: true,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "hopbox-gw: upstream error: "+err.Error(), http.StatusBadGateway)
		},
	}
	defer func() {
		if !used {
			_ = upstream.Close()
		}
	}()
	proxy.ServeHTTP(w, r)
}
