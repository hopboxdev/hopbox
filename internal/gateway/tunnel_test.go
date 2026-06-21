package gateway_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hopboxdev/hopbox/internal/gateway"
)

// startTunnel runs a TunnelServer backed by connector on a fresh listener and
// returns its address.
func startTunnel(t *testing.T, connector gateway.Connector) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := gateway.NewTunnelServer(connector)
	go func() { _ = srv.Serve(ctx, ln) }()
	return ln.Addr().String()
}

func TestRemoteTunnelEndToEnd(t *testing.T) {
	// the workspace service
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "served "+r.Host+r.URL.Path)
	}))
	defer backend.Close()
	backendAddr := backend.Listener.Addr().String()

	// hopboxd-side connector: resolves one host to the backend
	tunnelAddr := startTunnel(t, connectorFunc(func(_ context.Context, host string) (net.Conn, error) {
		if host == "app-w1.gw.example.com" {
			return net.Dial("tcp", backendAddr)
		}
		return nil, gateway.ErrNoRoute
	}))

	// standalone hopbox-gw: a gateway fronted by the RemoteConnector (stateless)
	gw := httptest.NewServer(gateway.New(gateway.NewRemoteConnector(tunnelAddr)))
	defer gw.Close()

	req, _ := http.NewRequest("GET", gw.URL+"/x", nil)
	req.Host = "app-w1.gw.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "served app-w1.gw.example.com/x" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestRemoteTunnelNoRouteIs404(t *testing.T) {
	tunnelAddr := startTunnel(t, connectorFunc(func(context.Context, string) (net.Conn, error) {
		return nil, gateway.ErrNoRoute
	}))
	gw := httptest.NewServer(gateway.New(gateway.NewRemoteConnector(tunnelAddr)))
	defer gw.Close()

	req, _ := http.NewRequest("GET", gw.URL+"/", nil)
	req.Host = "nobody.gw.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestRemoteConnectorNoRouteError(t *testing.T) {
	tunnelAddr := startTunnel(t, connectorFunc(func(context.Context, string) (net.Conn, error) {
		return nil, gateway.ErrNoRoute
	}))
	rc := gateway.NewRemoteConnector(tunnelAddr)
	if _, err := rc.Connect(context.Background(), "x.gw"); err != gateway.ErrNoRoute {
		t.Fatalf("want ErrNoRoute, got %v", err)
	}
}
