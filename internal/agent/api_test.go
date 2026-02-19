package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/agent"
	"github.com/hopboxdev/hopbox/internal/service"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// newTestAgent creates an Agent backed by a stub tunnel (no real WireGuard).
func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cfg := tunnel.Config{
		PrivateKey:    kp.PrivateKeyHex(),
		PeerPublicKey: peer.PublicKeyHex(),
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    0,
		MTU:           tunnel.DefaultMTU,
	}
	return agent.New(cfg)
}

// newTestServer starts the agent's HTTP API on a random port and returns the
// server and a base URL. The server is closed when the test ends.
func newTestServer(t *testing.T, a *agent.Agent) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(agent.Handler(a))
	t.Cleanup(srv.Close)
	return srv
}

func TestHealthEndpoint(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	resp, err := http.Post(srv.URL+"/health", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestRPCUnknownMethod(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{"method": "no.such.method"})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	var rpcResp struct{ Error string }
	_ = json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error == "" {
		t.Error("expected non-empty error field")
	}
}

func TestRPCInvalidJSON(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	resp, err := http.Post(srv.URL+"/rpc", "application/json",
		bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRPCServicesListEmpty(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{"method": "services.list"})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var rpcResp struct {
		Result []any  `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}
	if rpcResp.Error != "" {
		t.Errorf("unexpected error: %s", rpcResp.Error)
	}
}

func TestRPCServicesListWithManager(t *testing.T) {
	a := newTestAgent(t)

	sm := service.NewManager()
	sm.Register(
		&service.ServiceDef{Name: "web", Type: "docker"},
		&stubBackend{running: true},
	)
	a.WithServices(sm)

	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{"method": "services.list"})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}
	if len(rpcResp.Result) != 1 {
		t.Errorf("result len = %d, want 1", len(rpcResp.Result))
	}
	if rpcResp.Result[0]["name"] != "web" {
		t.Errorf("name = %v, want web", rpcResp.Result[0]["name"])
	}
}

func TestRPCServicesRestartMissingName(t *testing.T) {
	a := newTestAgent(t)
	a.WithServices(service.NewManager())
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{
		"method": "services.restart",
		"params": map[string]any{},
	})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRPCServicesRestartNoManager(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{
		"method": "services.restart",
		"params": map[string]string{"name": "web"},
	})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRPCServicesStopNoManager(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{
		"method": "services.stop",
		"params": map[string]string{"name": "web"},
	})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRPCPortsList(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	body, _ := json.Marshal(map[string]any{"method": "ports.list"})
	resp, err := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should return 200 (empty list on macOS, or real ports on Linux)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRPCMethodNotAllowed(t *testing.T) {
	a := newTestAgent(t)
	srv := newTestServer(t, a)

	resp, err := http.Get(srv.URL + "/rpc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// TestAgentRun verifies the agent starts and its /health endpoint responds.
func TestAgentRun(t *testing.T) {
	kp, _ := wgkey.Generate()
	peer, _ := wgkey.Generate()
	cfg := tunnel.Config{
		PrivateKey:    kp.PrivateKeyHex(),
		PeerPublicKey: peer.PublicKeyHex(),
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    0,
		MTU:           tunnel.DefaultMTU,
	}

	a := agent.New(cfg)

	// Give agent a random free port so it doesn't need the WireGuard IP.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- a.RunOnAddr(ctx, "127.0.0.1", port)
	}()

	// Poll until the server responds.
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("agent never responded: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not return after cancel")
	}
}

// stubBackend is a no-op service backend for testing.
type stubBackend struct {
	running bool
	startFn func() error
	stopFn  func() error
}

func (s *stubBackend) Start(_ context.Context, _ string) error {
	if s.startFn != nil {
		return s.startFn()
	}
	s.running = true
	return nil
}

func (s *stubBackend) Stop(_ string) error {
	if s.stopFn != nil {
		return s.stopFn()
	}
	s.running = false
	return nil
}

func (s *stubBackend) IsRunning(_ string) (bool, error) {
	return s.running, nil
}
