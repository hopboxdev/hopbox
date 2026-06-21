package gateway_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mesadev/mesa/internal/gateway"
)

type routerFunc func(host string) (string, int32, bool)

func (f routerFunc) Lookup(host string) (string, int32, bool) { return f(host) }

type dialerFunc func(ctx context.Context, wsID string, port int32) (net.Conn, error)

func (f dialerFunc) DialWorkspace(ctx context.Context, wsID string, port int32) (net.Conn, error) {
	return f(ctx, wsID, port)
}

func TestGatewayProxiesToWorkspaceService(t *testing.T) {
	// stand-in for the user's dev server running inside a workspace
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.Host+r.URL.Path)
	}))
	defer backend.Close()
	backendAddr := backend.Listener.Addr().String()

	g := gateway.New(
		routerFunc(func(host string) (string, int32, bool) {
			if host == "app-w1.gw.example.com" {
				return "w1", 3000, true
			}
			return "", 0, false
		}),
		// the dialer stands in for hub.OpenForward: it returns a conn to the
		// backend regardless of port, exactly as the agent forward stream would.
		dialerFunc(func(_ context.Context, _ string, _ int32) (net.Conn, error) {
			return net.Dial("tcp", backendAddr)
		}),
	)
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
	g := gateway.New(
		routerFunc(func(string) (string, int32, bool) { return "", 0, false }),
		dialerFunc(func(context.Context, string, int32) (net.Conn, error) { return nil, nil }),
	)
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
