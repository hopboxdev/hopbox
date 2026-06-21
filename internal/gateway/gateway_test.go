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

type connectorFunc func(ctx context.Context, host string) (net.Conn, error)

func (f connectorFunc) Connect(ctx context.Context, host string) (net.Conn, error) {
	return f(ctx, host)
}

func TestGatewayProxiesToWorkspaceService(t *testing.T) {
	// stand-in for the user's dev server running inside a workspace
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.Host+r.URL.Path)
	}))
	defer backend.Close()
	backendAddr := backend.Listener.Addr().String()

	// the connector stands in for the in-proc/remote path: it returns a conn to
	// the backend, exactly as an agent forward stream would.
	g := gateway.New(connectorFunc(func(_ context.Context, host string) (net.Conn, error) {
		if host == "app-w1.gw.example.com" {
			return net.Dial("tcp", backendAddr)
		}
		return nil, gateway.ErrNoRoute
	}))
	gw := httptest.NewServer(g)
	defer gw.Close()

	req, _ := http.NewRequest("GET", gw.URL+"/ping", nil)
	req.Host = "app-w1.gw.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if string(body) != "hello from app-w1.gw.example.com/ping" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestGatewayUnknownHostIs404(t *testing.T) {
	g := gateway.New(connectorFunc(func(context.Context, string) (net.Conn, error) {
		return nil, gateway.ErrNoRoute
	}))
	gw := httptest.NewServer(g)
	defer gw.Close()

	req, _ := http.NewRequest("GET", gw.URL+"/", nil)
	req.Host = "nobody.gw.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for unknown host, got %d", resp.StatusCode)
	}
}
