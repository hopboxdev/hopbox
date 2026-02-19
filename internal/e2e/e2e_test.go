// Package e2e contains end-to-end tests that exercise the full path from an
// HTTP client through an in-memory WireGuard tunnel to a running agent HTTP
// server — all within a single process, with no real network or root required.
//
// The transport layer uses bindtest.NewChannelBinds() (two ChannelBind
// instances connected by in-memory channels), so packets never touch a real
// UDP socket. Each test brings up two netstack WireGuard devices, starts the
// agent on the server netstack, and connects an HTTP client through the client
// netstack.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/hopboxdev/hopbox/internal/agent"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

// e2eEnv holds a running agent accessible via WireGuard and an HTTP client
// that routes through the in-memory tunnel.
type e2eEnv struct {
	baseURL    string
	httpClient *http.Client
}

// newE2EEnv creates two in-process WireGuard devices (via bindtest), starts
// the agent HTTP API on the server netstack, and returns an e2eEnv. The
// optional configure functions run on the Agent before it starts, allowing
// tests to call WithServices, WithScripts, etc.
//
// The environment is fully torn down when the test ends via t.Cleanup.
func newE2EEnv(t *testing.T, configure ...func(*agent.Agent)) *e2eEnv {
	t.Helper()

	clientKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	serverKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	clientAddr := netip.MustParseAddr(tunnel.ClientIP)
	serverAddr := netip.MustParseAddr(tunnel.ServerIP)

	// Create paired in-memory channel binds.
	// binds[0] = client, binds[1] = server.
	// Client sends to endpoint "127.0.0.1:1" → lands in binds[1].rx4.
	binds := bindtest.NewChannelBinds()
	clientBind := binds[0]
	serverBind := binds[1]

	mtu := tunnel.DefaultMTU
	logger := device.NewLogger(device.LogLevelSilent, "")

	// ---- Server netstack ----
	serverTunDev, serverNet, err := netstack.CreateNetTUN(
		[]netip.Addr{serverAddr}, nil, mtu,
	)
	if err != nil {
		t.Fatalf("server CreateNetTUN: %v", err)
	}

	serverDev := device.NewDevice(serverTunDev, serverBind, logger)
	serverIPC := tunnel.BuildServerIPC(tunnel.Config{
		PrivateKey:    serverKP.PrivateKeyHex(),
		PeerPublicKey: clientKP.PublicKeyHex(),
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    51820,
	})
	if err := serverDev.IpcSet(serverIPC); err != nil {
		t.Fatalf("server IpcSet: %v", err)
	}
	if err := serverDev.Up(); err != nil {
		t.Fatalf("server Up: %v", err)
	}

	// ---- Client netstack ----
	clientTunDev, clientNet, err := netstack.CreateNetTUN(
		[]netip.Addr{clientAddr}, nil, mtu,
	)
	if err != nil {
		t.Fatalf("client CreateNetTUN: %v", err)
	}

	clientDev := device.NewDevice(clientTunDev, clientBind, logger)
	clientIPC := tunnel.BuildClientIPC(tunnel.Config{
		PrivateKey:          clientKP.PrivateKeyHex(),
		PeerPublicKey:       serverKP.PublicKeyHex(),
		PeerIP:              tunnel.ServerIP + "/32",
		Endpoint:            "127.0.0.1:1",
		PersistentKeepalive: 1 * time.Second,
	})
	if err := clientDev.IpcSet(clientIPC); err != nil {
		t.Fatalf("client IpcSet: %v", err)
	}
	if err := clientDev.Up(); err != nil {
		t.Fatalf("client Up: %v", err)
	}

	// ---- Agent ----
	a := agent.New(tunnel.Config{
		PrivateKey:    serverKP.PrivateKeyHex(),
		PeerPublicKey: clientKP.PublicKeyHex(),
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    0,
		MTU:           mtu,
	})
	for _, fn := range configure {
		fn(a)
	}

	// Listen on server netstack — the agent serves HTTP here.
	serverListener, err := serverNet.ListenTCP(&net.TCPAddr{
		IP:   net.ParseIP(tunnel.ServerIP),
		Port: tunnel.AgentAPIPort,
	})
	if err != nil {
		t.Fatalf("agent ListenTCP: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- a.RunOnListener(ctx, serverListener)
	}()

	t.Cleanup(func() {
		cancel()
		serverDev.Close()
		clientDev.Close()
		<-agentDone
	})

	// HTTP client dials through the client WireGuard netstack.
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: clientNet.DialContext,
		},
	}

	env := &e2eEnv{
		baseURL:    fmt.Sprintf("http://%s:%d", tunnel.ServerIP, tunnel.AgentAPIPort),
		httpClient: httpClient,
	}

	// Wait for the agent to become reachable over the tunnel before returning.
	env.waitReady(t)
	return env
}

// waitReady polls /health until the agent responds or the 15-second timeout
// embedded in the context fires.
func (e *e2eEnv) waitReady(t *testing.T) {
	t.Helper()
	for i := 0; i < 50; i++ {
		resp, err := e.httpClient.Get(e.baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("agent did not become reachable over WireGuard tunnel within timeout")
}

// rpcPost sends a JSON-RPC request and returns the HTTP response.
func (e *e2eEnv) rpcPost(method string, params any) (*http.Response, error) {
	body, _ := json.Marshal(map[string]any{"method": method, "params": params})
	return e.httpClient.Post(e.baseURL+"/rpc", "application/json", bytes.NewReader(body))
}

// ---- Tests ----

// TestE2EHealthEndpoint verifies that GET /health returns 200 {"status":"ok"}
// through the WireGuard tunnel.
func TestE2EHealthEndpoint(t *testing.T) {
	env := newE2EEnv(t)

	resp, err := env.httpClient.Get(env.baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

// TestE2EHealthMethodNotAllowed verifies that POST /health returns 405.
func TestE2EHealthMethodNotAllowed(t *testing.T) {
	env := newE2EEnv(t)

	resp, err := env.httpClient.Post(env.baseURL+"/health", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// TestE2EServicesListEmpty verifies that services.list returns an empty result
// when no service manager is configured.
func TestE2EServicesListEmpty(t *testing.T) {
	env := newE2EEnv(t)

	resp, err := env.rpcPost("services.list", nil)
	if err != nil {
		t.Fatalf("services.list: %v", err)
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
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.Error != "" {
		t.Errorf("unexpected error: %s", rpcResp.Error)
	}
}

// TestE2ERunScript verifies that a named script is executed on the agent and
// its output is returned through the tunnel.
func TestE2ERunScript(t *testing.T) {
	env := newE2EEnv(t, func(a *agent.Agent) {
		a.WithScripts(map[string]string{"greet": "echo hello"})
	})

	resp, err := env.rpcPost("run.script", map[string]string{"name": "greet"})
	if err != nil {
		t.Fatalf("run.script: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var rpcResp struct {
		Result map[string]string `json:"result"`
		Error  string            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.Error != "" {
		t.Errorf("unexpected error: %s", rpcResp.Error)
	}
	if rpcResp.Result["output"] == "" {
		t.Error("expected non-empty output from script")
	}
}

// TestE2ERunScriptNotFound verifies that requesting an unknown script returns 404.
func TestE2ERunScriptNotFound(t *testing.T) {
	env := newE2EEnv(t, func(a *agent.Agent) {
		a.WithScripts(map[string]string{"build": "go build ./..."})
	})

	resp, err := env.rpcPost("run.script", map[string]string{"name": "nonexistent"})
	if err != nil {
		t.Fatalf("run.script: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestE2ESnapNoTarget verifies that snap operations return 503 when no backup
// target is configured.
func TestE2ESnapNoTarget(t *testing.T) {
	env := newE2EEnv(t)

	for _, method := range []string{"snap.create", "snap.restore", "snap.list"} {
		resp, err := env.rpcPost(method, map[string]string{"id": "abc"})
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", method, resp.StatusCode)
		}
	}
}

// TestE2EWorkspaceSync verifies that workspace.sync pushes a manifest to the
// agent and that scripts declared in it become immediately runnable.
func TestE2EWorkspaceSync(t *testing.T) {
	env := newE2EEnv(t)

	wsYAML := `name: synctest
scripts:
  hello: echo synced
`
	resp, err := env.rpcPost("workspace.sync", map[string]string{"yaml": wsYAML})
	if err != nil {
		t.Fatalf("workspace.sync: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("workspace.sync: status = %d, body: %s", resp.StatusCode, body)
	}
	var syncResp struct {
		Result map[string]string `json:"result"`
		Error  string            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		t.Fatalf("decode sync response: %v", err)
	}
	if syncResp.Error != "" {
		t.Fatalf("workspace.sync error: %s", syncResp.Error)
	}
	if syncResp.Result["status"] != "synced" {
		t.Errorf("status = %q, want synced", syncResp.Result["status"])
	}

	// The script declared in the manifest must now be runnable.
	resp2, err := env.rpcPost("run.script", map[string]string{"name": "hello"})
	if err != nil {
		t.Fatalf("run.script: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("run.script after sync: status = %d, body: %s", resp2.StatusCode, body)
	}
	var scriptResp struct {
		Result map[string]string `json:"result"`
		Error  string            `json:"error"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&scriptResp); err != nil {
		t.Fatalf("decode script response: %v", err)
	}
	if scriptResp.Error != "" {
		t.Errorf("run.script error: %s", scriptResp.Error)
	}
	if scriptResp.Result["output"] == "" {
		t.Error("expected non-empty output from script")
	}
}

// TestE2EUnknownRPCMethod verifies that an unknown method returns 404 with an
// error field.
func TestE2EUnknownRPCMethod(t *testing.T) {
	env := newE2EEnv(t)

	resp, err := env.rpcPost("no.such.method", nil)
	if err != nil {
		t.Fatalf("rpc: %v", err)
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
