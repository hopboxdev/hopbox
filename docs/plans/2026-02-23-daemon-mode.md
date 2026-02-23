# Daemon Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor `hop up` into a background daemon with `hop down` teardown and `hop daemon` subcommands.

**Architecture:** A new `internal/daemon/` package owns the long-lived tunnel lifecycle (TUN, WireGuard, bridges, ConnMonitor) and exposes a Unix socket for IPC. `hop up` becomes the interactive frontend that launches the daemon then runs TUI phases. `hop down` sends a shutdown command via socket.

**Tech Stack:** Go stdlib (`net`, `encoding/json`, `os/exec`, `syscall`), Kong CLI framework, existing `internal/tunnel`, `internal/bridge`, `internal/helper` packages.

**Design doc:** `docs/plans/2026-02-23-daemon-mode-design.md`

---

### Task 1: Daemon socket protocol types

**Files:**
- Create: `internal/daemon/protocol.go`
- Test: `internal/daemon/protocol_test.go`

**Step 1: Write the failing test**

```go
// internal/daemon/protocol_test.go
package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{Method: "shutdown"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "shutdown" {
		t.Errorf("Method = %q, want %q", decoded.Method, "shutdown")
	}
}

func TestResponseMarshal(t *testing.T) {
	resp := Response{
		OK: true,
		State: &DaemonStatus{
			PID:         1234,
			Connected:   true,
			LastHealthy: time.Now().Truncate(time.Second),
			Interface:   "utun5",
			StartedAt:   time.Now().Truncate(time.Second),
			Bridges:     []string{"clipboard", "cdp"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK {
		t.Error("OK = false, want true")
	}
	if decoded.State == nil {
		t.Fatal("State is nil")
	}
	if decoded.State.PID != 1234 {
		t.Errorf("PID = %d, want 1234", decoded.State.PID)
	}
	if len(decoded.State.Bridges) != 2 {
		t.Errorf("Bridges len = %d, want 2", len(decoded.State.Bridges))
	}
}

func TestErrorResponse(t *testing.T) {
	resp := Response{OK: false, Error: "not running"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OK {
		t.Error("OK = true, want false")
	}
	if decoded.Error != "not running" {
		t.Errorf("Error = %q, want %q", decoded.Error, "not running")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/... -run TestRequestMarshal -v`
Expected: FAIL — package does not exist yet

**Step 3: Write minimal implementation**

```go
// internal/daemon/protocol.go
package daemon

import "time"

// Request is sent from a client (hop up, hop down) to the daemon over the Unix socket.
type Request struct {
	Method string `json:"method"` // "status" or "shutdown"
}

// Response is sent from the daemon back to the client.
type Response struct {
	OK    bool          `json:"ok"`
	Error string        `json:"error,omitempty"`
	State *DaemonStatus `json:"state,omitempty"`
}

// DaemonStatus is the live state returned by the "status" method.
type DaemonStatus struct {
	PID         int       `json:"pid"`
	Connected   bool      `json:"connected"`
	LastHealthy time.Time `json:"last_healthy,omitempty"`
	Interface   string    `json:"interface"`
	StartedAt   time.Time `json:"started_at"`
	Bridges     []string  `json:"bridges"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/protocol.go internal/daemon/protocol_test.go
git commit -m "feat(daemon): add socket protocol types"
```

---

### Task 2: Daemon socket client

**Files:**
- Create: `internal/daemon/client.go`
- Create: `internal/daemon/client_test.go`

The client connects to the daemon's Unix socket, sends a JSON request, reads a JSON response, and closes. Also includes `SocketPath()` helper and `WaitForReady()` polling.

**Step 1: Write the failing test**

```go
// internal/daemon/client_test.go
package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startMockServer creates a Unix socket server that responds to one request.
func startMockServer(t *testing.T, sockPath string, handler func(Request) Response) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				var req Request
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				resp := handler(req)
				_ = json.NewEncoder(c).Encode(resp)
			}(conn)
		}
	}()
	return ln
}

func TestClientStatus(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln := startMockServer(t, sockPath, func(req Request) Response {
		if req.Method != "status" {
			return Response{OK: false, Error: "unexpected method"}
		}
		return Response{OK: true, State: &DaemonStatus{
			PID:       1234,
			Connected: true,
			Interface: "utun5",
			Bridges:   []string{"clipboard"},
		}}
	})
	defer func() { _ = ln.Close() }()

	c := &Client{SocketPath: sockPath}
	status, err := c.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.PID != 1234 {
		t.Errorf("PID = %d, want 1234", status.PID)
	}
	if !status.Connected {
		t.Error("Connected = false, want true")
	}
}

func TestClientShutdown(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	shutdownCalled := make(chan struct{})
	ln := startMockServer(t, sockPath, func(req Request) Response {
		if req.Method == "shutdown" {
			close(shutdownCalled)
			return Response{OK: true}
		}
		return Response{OK: false, Error: "unexpected"}
	})
	defer func() { _ = ln.Close() }()

	c := &Client{SocketPath: sockPath}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shutdownCalled:
	case <-time.After(time.Second):
		t.Fatal("shutdown handler was not called")
	}
}

func TestClientNotRunning(t *testing.T) {
	c := &Client{SocketPath: "/tmp/definitely-not-a-socket-hopbox-test.sock"}
	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error for non-existent socket")
	}
}

func TestSocketPath(t *testing.T) {
	path, err := SocketPath("myhost")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "hopbox", "run", "myhost.sock")
	if path != want {
		t.Errorf("SocketPath = %q, want %q", path, want)
	}
}

func TestWaitForReady(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	// Start server after a delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			return
		}
		defer func() { _ = ln.Close() }()
		// Accept one connection and respond.
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var req Request
		_ = json.NewDecoder(conn).Decode(&req)
		_ = json.NewEncoder(conn).Encode(Response{OK: true, State: &DaemonStatus{PID: 42}})
	}()

	c := &Client{SocketPath: sockPath}
	err := c.WaitForReady(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForReady: %v", err)
	}
}

func TestWaitForReadyTimeout(t *testing.T) {
	c := &Client{SocketPath: "/tmp/definitely-not-a-socket-hopbox-test.sock"}
	err := c.WaitForReady(300 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/... -run TestClient -v`
Expected: FAIL — `Client` type doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/daemon/client.go
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Client communicates with a running daemon over its Unix socket.
type Client struct {
	SocketPath string
}

// NewClient returns a Client for the given host name.
func NewClient(hostName string) (*Client, error) {
	path, err := SocketPath(hostName)
	if err != nil {
		return nil, err
	}
	return &Client{SocketPath: path}, nil
}

// SocketPath returns ~/.config/hopbox/run/<host>.sock.
func SocketPath(hostName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox", "run", hostName+".sock"), nil
}

// call sends a request and returns the response.
func (c *Client) call(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon at %s: %w", c.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: %s", resp.Error)
	}
	return &resp, nil
}

// Status queries the daemon for its current state.
func (c *Client) Status() (*DaemonStatus, error) {
	resp, err := c.call(Request{Method: "status"})
	if err != nil {
		return nil, err
	}
	return resp.State, nil
}

// Shutdown asks the daemon to gracefully shut down.
func (c *Client) Shutdown() error {
	_, err := c.call(Request{Method: "shutdown"})
	return err
}

// IsRunning returns true if the daemon socket is connectable.
func (c *Client) IsRunning() bool {
	conn, err := net.DialTimeout("unix", c.SocketPath, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// WaitForReady polls the daemon socket until it accepts a status request
// or the timeout expires.
func (c *Client) WaitForReady(timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("daemon not ready within %s", timeout)
		case <-ticker.C:
			if _, err := c.Status(); err == nil {
				return nil
			}
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/client.go internal/daemon/client_test.go
git commit -m "feat(daemon): add socket client with status, shutdown, and readiness polling"
```

---

### Task 3: Daemon socket server

**Files:**
- Create: `internal/daemon/server.go`
- Create: `internal/daemon/server_test.go`

The server listens on a Unix socket and dispatches incoming JSON requests to a handler.

**Step 1: Write the failing test**

```go
// internal/daemon/server_test.go
package daemon

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestServerHandlesStatus(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	handler := &MockHandler{
		status: &DaemonStatus{PID: 9999, Connected: true, Interface: "utun3"},
	}
	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	c := &Client{SocketPath: sockPath}
	status, err := c.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.PID != 9999 {
		t.Errorf("PID = %d, want 9999", status.PID)
	}
}

func TestServerHandlesShutdown(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	handler := &MockHandler{
		status:     &DaemonStatus{PID: 1},
		shutdownCh: make(chan struct{}, 1),
	}
	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	c := &Client{SocketPath: sockPath}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-handler.shutdownCh:
	case <-time.After(time.Second):
		t.Fatal("shutdown was not called")
	}
}

func TestServerRejectsUnknownMethod(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv := NewServer(sockPath, &MockHandler{status: &DaemonStatus{}})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	// Send raw request with unknown method.
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = json.NewEncoder(conn).Encode(Request{Method: "restart"})
	var resp Response
	_ = json.NewDecoder(conn).Decode(&resp)
	if resp.OK {
		t.Error("expected error response for unknown method")
	}
}

func TestServerCleanupSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv := NewServer(sockPath, &MockHandler{status: &DaemonStatus{}})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	srv.Stop()

	// Socket file should be removed.
	conn, err := net.DialTimeout("unix", sockPath, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Error("socket should not be connectable after Stop")
	}
}

// MockHandler implements the Handler interface for testing.
type MockHandler struct {
	status     *DaemonStatus
	shutdownCh chan struct{}
}

func (h *MockHandler) HandleStatus() *DaemonStatus {
	return h.status
}

func (h *MockHandler) HandleShutdown() {
	if h.shutdownCh != nil {
		select {
		case h.shutdownCh <- struct{}{}:
		default:
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/... -run TestServer -v`
Expected: FAIL — `Server`, `Handler`, `NewServer` don't exist

**Step 3: Write minimal implementation**

```go
// internal/daemon/server.go
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// Handler is implemented by the daemon to respond to IPC requests.
type Handler interface {
	HandleStatus() *DaemonStatus
	HandleShutdown()
}

// Server listens on a Unix socket and dispatches requests to a Handler.
type Server struct {
	sockPath string
	handler  Handler
	listener net.Listener
	wg       sync.WaitGroup
}

// NewServer creates a new IPC server.
func NewServer(sockPath string, handler Handler) *Server {
	return &Server{sockPath: sockPath, handler: handler}
}

// Start begins accepting connections. Non-blocking — runs in background.
func (s *Server) Start() error {
	// Remove stale socket file if it exists.
	_ = os.Remove(s.sockPath)

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.sockPath, err)
	}
	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go s.handle(conn)
		}
	}()
	return nil
}

// Stop closes the listener and removes the socket file.
func (s *Server) Stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	_ = os.Remove(s.sockPath)
}

func (s *Server) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "invalid request"})
		return
	}

	switch req.Method {
	case "status":
		status := s.handler.HandleStatus()
		_ = json.NewEncoder(conn).Encode(Response{OK: true, State: status})
	case "shutdown":
		_ = json.NewEncoder(conn).Encode(Response{OK: true})
		s.handler.HandleShutdown()
	default:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: fmt.Sprintf("unknown method: %s", req.Method)})
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/server.go internal/daemon/server_test.go
git commit -m "feat(daemon): add socket server with handler interface"
```

---

### Task 4: Daemon core — Run function

**Files:**
- Create: `internal/daemon/daemon.go`

This is the main daemon loop. It creates the TUN device, starts WireGuard, bridges, ConnMonitor, and the IPC server. Extracted from `cmd/hop/up.go` lines 52-274.

No unit test for this task — it requires the helper daemon (privileged). The code is a composition of already-tested components. Integration tested via manual `hop daemon start`.

**Step 1: Write implementation**

```go
// internal/daemon/daemon.go
package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

const (
	agentClientTimeout = 5 * time.Second
)

// Config holds everything the daemon needs to start.
type Config struct {
	HostName string
	TunCfg   tunnel.Config
	Manifest *manifest.Workspace // nil if no workspace
}

// Daemon manages the tunnel lifecycle.
type Daemon struct {
	cfg       Config
	tun       *tunnel.KernelTunnel
	monitor   *tunnel.ConnMonitor
	bridges   []bridge.Bridge
	state     *tunnel.TunnelState
	server    *Server
	cancel    context.CancelFunc
	mu        sync.Mutex
}

// Run starts the daemon and blocks until shutdown.
// This is the main entry point for `hop daemon start`.
func Run(cfg Config) error {
	// Ignore SIGHUP so the daemon survives terminal close.
	signal.Ignore(syscall.SIGHUP)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		return fmt.Errorf("hopbox helper is not running; install with 'sudo hop-helper --install'")
	}

	// Create TUN device via helper.
	tunFile, ifName, err := helperClient.CreateTUN(cfg.TunCfg.MTU)
	if err != nil {
		return fmt.Errorf("create TUN device: %w", err)
	}

	tun := tunnel.NewKernelTunnel(cfg.TunCfg, tunFile, ifName)

	tunnelErr := make(chan error, 1)
	go func() {
		tunnelErr <- tun.Start(ctx)
	}()

	// Wait for TUN ready.
	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Configure TUN (IP + route) via helper.
	localIP := strings.TrimSuffix(cfg.TunCfg.LocalIP, "/24")
	peerIP := strings.TrimSuffix(cfg.TunCfg.PeerIP, "/32")
	if err := helperClient.ConfigureTUN(tun.InterfaceName(), localIP, peerIP); err != nil {
		tun.Stop()
		return fmt.Errorf("configure TUN: %w", err)
	}
	defer func() { _ = helperClient.CleanupTUN(tun.InterfaceName()) }()

	hostname := cfg.HostName + ".hop"
	if err := helperClient.AddHost(peerIP, hostname); err != nil {
		tun.Stop()
		return fmt.Errorf("add host entry: %w", err)
	}
	defer func() { _ = helperClient.RemoveHost(hostname) }()

	log.Printf("interface %s up, %s → %s", tun.InterfaceName(), localIP, hostname)

	// Start bridges.
	var bridges []bridge.Bridge
	if cfg.Manifest != nil {
		for _, b := range cfg.Manifest.Bridges {
			switch b.Type {
			case "clipboard":
				br := bridge.NewClipboardBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						log.Printf("clipboard bridge error: %v", err)
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						log.Printf("CDP bridge error: %v", err)
					}
				}(br)
			}
		}
	}

	// Write state file.
	state := &tunnel.TunnelState{
		PID:         os.Getpid(),
		Host:        cfg.HostName,
		Hostname:    hostname,
		Interface:   tun.InterfaceName(),
		StartedAt:   time.Now(),
		Connected:   true,
		LastHealthy: time.Now(),
	}
	if err := tunnel.WriteState(state); err != nil {
		log.Printf("write tunnel state: %v", err)
	}
	defer func() { _ = tunnel.RemoveState(cfg.HostName) }()

	// Start ConnMonitor.
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)
	agentClient := &http.Client{Timeout: agentClientTimeout}

	monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: agentURL,
		Client:    agentClient,
		OnStateChange: func(evt tunnel.ConnEvent) {
			switch evt.State {
			case tunnel.ConnStateDisconnected:
				log.Printf("agent unreachable — waiting for reconnection...")
				state.Connected = false
			case tunnel.ConnStateConnected:
				log.Printf("agent reconnected (was down for %s)", evt.Duration.Round(time.Second))
				state.Connected = true
				state.LastHealthy = evt.At
			}
			if err := tunnel.WriteState(state); err != nil {
				log.Printf("update tunnel state: %v", err)
			}
		},
		OnHealthy: func(t time.Time) {
			state.LastHealthy = t
			if err := tunnel.WriteState(state); err != nil {
				log.Printf("update tunnel state: %v", err)
			}
		},
	})
	go monitor.Run(ctx)

	// Build daemon handler for IPC.
	d := &daemonHandler{
		state:   state,
		bridges: bridges,
		cancel:  cancel,
	}

	// Start IPC server.
	sockPath, err := SocketPath(cfg.HostName)
	if err != nil {
		return fmt.Errorf("socket path: %w", err)
	}
	srv := NewServer(sockPath, d)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start IPC server: %w", err)
	}
	defer srv.Stop()

	log.Printf("daemon ready (PID %d)", os.Getpid())

	// Block until shutdown.
	select {
	case <-ctx.Done():
		log.Println("shutting down...")
	case err := <-tunnelErr:
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	}

	return nil
}

// daemonHandler implements Handler for the IPC server.
type daemonHandler struct {
	state   *tunnel.TunnelState
	bridges []bridge.Bridge
	cancel  context.CancelFunc
}

func (d *daemonHandler) HandleStatus() *DaemonStatus {
	var bridgeNames []string
	for _, b := range d.bridges {
		bridgeNames = append(bridgeNames, b.Status())
	}
	return &DaemonStatus{
		PID:         d.state.PID,
		Connected:   d.state.Connected,
		LastHealthy: d.state.LastHealthy,
		Interface:   d.state.Interface,
		StartedAt:   d.state.StartedAt,
		Bridges:     bridgeNames,
	}
}

func (d *daemonHandler) HandleShutdown() {
	d.cancel()
}
```

**Step 2: Verify compilation**

Run: `go build ./internal/daemon/...`
Expected: BUILD OK

**Step 3: Run all daemon tests**

Run: `go test ./internal/daemon/... -v`
Expected: PASS (existing protocol + client + server tests still pass)

**Step 4: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat(daemon): add Run function with tunnel, bridge, monitor, and IPC lifecycle"
```

---

### Task 5: Kong `hop daemon` subcommands

**Files:**
- Create: `cmd/hop/daemon.go`
- Modify: `cmd/hop/main.go:17-37` — add Daemon field to CLI struct

**Step 1: Write the daemon command file**

```go
// cmd/hop/daemon.go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/hopboxdev/hopbox/internal/daemon"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// DaemonCmd manages the tunnel daemon.
type DaemonCmd struct {
	Start  DaemonStartCmd  `cmd:"" help:"Start tunnel daemon for a host."`
	Stop   DaemonStopCmd   `cmd:"" help:"Stop tunnel daemon for a host."`
	Status DaemonStatusCmd `cmd:"" help:"Show daemon status for a host."`
}

// DaemonStartCmd starts the daemon process. Runs in foreground (intended to be
// launched by hop up as a detached child, or directly by power users).
type DaemonStartCmd struct {
	Host      string `arg:"" help:"Host name to start daemon for."`
	Workspace string `help:"Path to hopbox.yaml for bridge configuration." type:"existingfile"`
}

func (c *DaemonStartCmd) Run() error {
	cfg, err := hostconfig.Load(c.Host)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", c.Host, err)
	}

	// Check if daemon is already running.
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	if status, err := client.Status(); err == nil {
		return fmt.Errorf("daemon already running for %q (PID %d)", c.Host, status.PID)
	}

	tunCfg, err := cfg.ToTunnelConfig()
	if err != nil {
		return fmt.Errorf("convert tunnel config: %w", err)
	}

	// Load manifest for bridge config.
	var ws *manifest.Workspace
	if c.Workspace != "" {
		ws, err = manifest.Parse(c.Workspace)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	return daemon.Run(daemon.Config{
		HostName: c.Host,
		TunCfg:   tunCfg,
		Manifest: ws,
	})
}

// DaemonStopCmd sends a shutdown command to a running daemon.
type DaemonStopCmd struct {
	Host string `arg:"" help:"Host name to stop daemon for."`
}

func (c *DaemonStopCmd) Run() error {
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("no tunnel running for %q", c.Host)
	}
	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel %s stopped", c.Host)))
	return nil
}

// DaemonStatusCmd queries a running daemon for its current state.
type DaemonStatusCmd struct {
	Host string `arg:"" help:"Host name to query."`
}

func (c *DaemonStatusCmd) Run() error {
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("no daemon running for %q", c.Host)
	}

	connStr := "no"
	if status.Connected {
		connStr = "yes"
	}
	fmt.Printf("PID:          %d\n", status.PID)
	fmt.Printf("Interface:    %s\n", status.Interface)
	fmt.Printf("Connected:    %s\n", connStr)
	if !status.LastHealthy.IsZero() {
		fmt.Printf("Last healthy: %s ago\n", time.Since(status.LastHealthy).Round(time.Second))
	}
	if !status.StartedAt.IsZero() {
		fmt.Printf("Uptime:       %s\n", time.Since(status.StartedAt).Round(time.Second))
	}
	if len(status.Bridges) > 0 {
		fmt.Printf("Bridges:      %v\n", status.Bridges)
	}
	_, _ = fmt.Fprintln(os.Stderr)
	return nil
}
```

**Step 2: Register in main.go**

Add `Daemon DaemonCmd` to the CLI struct in `cmd/hop/main.go`:

In `cmd/hop/main.go`, add after the `Down` field (line 23):
```go
Daemon  DaemonCmd `cmd:"" help:"Manage tunnel daemon."`
```

**Step 3: Verify compilation**

Run: `go build ./cmd/hop/...`
Expected: BUILD OK

**Step 4: Verify help output includes daemon**

Run: `go run ./cmd/hop/... daemon --help`
Expected: Shows start/stop/status subcommands

**Step 5: Commit**

```bash
git add cmd/hop/daemon.go cmd/hop/main.go
git commit -m "feat: add hop daemon start/stop/status subcommands"
```

---

### Task 6: Refactor `hop up` for daemon mode

**Files:**
- Modify: `cmd/hop/up.go` — add `--foreground` flag, default to daemon mode

This is the biggest refactoring step. The current `UpCmd.Run()` (lines 37-287) splits into:
- **Foreground path**: Same as today (used with `--foreground`)
- **Daemon path** (default): Launch daemon child, wait for readiness, run TUI phases

**Step 1: Modify UpCmd struct**

Add `Foreground` flag to `UpCmd` in `cmd/hop/up.go`:

```go
type UpCmd struct {
	Workspace  string `arg:"" optional:"" help:"Path to hopbox.yaml (default: ./hopbox.yaml)."`
	SSH        bool   `help:"Fall back to SSH tunneling instead of WireGuard."`
	Foreground bool   `short:"f" help:"Run in foreground (don't daemonize)."`
}
```

**Step 2: Write the daemon-mode launch logic**

Replace `UpCmd.Run()` body with:

```go
func (c *UpCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", hostName, err)
	}

	// Check for running daemon.
	daemonClient, err := daemon.NewClient(hostName)
	if err != nil {
		return err
	}
	daemonRunning := daemonClient.IsRunning()

	if c.Foreground {
		if daemonRunning {
			return fmt.Errorf("daemon already running for %q; use 'hop down' first, or run without --foreground", hostName)
		}
		return c.runForeground(globals, hostName, cfg)
	}

	// Daemon mode (default).
	if !daemonRunning {
		if err := c.launchDaemon(hostName); err != nil {
			return err
		}
		fmt.Println(ui.StepRun("Starting tunnel daemon..."))
		if err := daemonClient.WaitForReady(15 * time.Second); err != nil {
			return fmt.Errorf("daemon failed to start: %w", err)
		}
	} else {
		fmt.Println(ui.StepOK("Tunnel daemon already running"))
	}

	// Get daemon status for display.
	status, err := daemonClient.Status()
	if err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}
	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel to %s up (%s)", cfg.Name, status.Interface)))

	// Run TUI phases (agent probe, manifest sync, packages).
	return c.runTUIPhases(globals, hostName, cfg)
}
```

**Step 3: Extract foreground path**

Move current `Run()` body (lines 37-287) into `runForeground()` method. This preserves the existing behavior verbatim for `--foreground`.

```go
func (c *UpCmd) runForeground(globals *CLI, hostName string, cfg *hostconfig.HostConfig) error {
	// ... exact copy of current Run() body from line 48 onwards ...
	// (checking for existing tunnel, creating TUN, starting WireGuard,
	//  TUI phases, bridges, state, monitor, blocking on Ctrl-C)
}
```

**Step 4: Extract TUI phases**

Move the TUI phase logic (current lines 122-198) into `runTUIPhases()` method. This is shared between daemon mode and foreground mode.

```go
func (c *UpCmd) runTUIPhases(globals *CLI, hostName string, cfg *hostconfig.HostConfig) error {
	hostname := cfg.Name + ".hop"
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	ctx := context.Background()

	// Load workspace manifest.
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	var ws *manifest.Workspace
	if _, err := os.Stat(wsPath); err == nil {
		var err error
		ws, err = manifest.Parse(wsPath)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	var phases []tui.Phase

	// Agent phase.
	agentSteps := []tui.Step{
		{Title: fmt.Sprintf("Probing agent at %s", agentURL), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("agent probe failed: %w", err)
			}
			// ... version check same as before ...
			send(tui.StepEvent{Message: "Agent is up"})
			return nil
		}},
	}
	phases = append(phases, tui.Phase{Title: "Agent", Steps: agentSteps})

	// Workspace phase (optional).
	if ws != nil {
		// ... same as current lines 153-191 ...
	}

	if len(phases) > 0 {
		if err := tui.RunPhases(ctx, "hop up", phases); err != nil {
			return err
		}
	}

	fmt.Println(ui.StepOK("Tunnel ready"))
	return nil
}
```

**Step 5: Implement launchDaemon()**

```go
func (c *UpCmd) launchDaemon(hostName string) error {
	// Find the hop binary path.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	// Build command args.
	args := []string{"daemon", "start", hostName}
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	if _, err := os.Stat(wsPath); err == nil {
		abs, err := filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("resolve workspace path: %w", err)
		}
		args = append(args, "--workspace", abs)
	}

	// Open log file.
	logPath, err := daemon.LogPath(hostName)
	if err != nil {
		return err
	}
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch daemon: %w", err)
	}

	// Detach — don't wait for the child.
	go func() { _ = cmd.Wait() }()
	return nil
}
```

**Step 6: Add LogPath helper to daemon package**

Add to `internal/daemon/client.go`:
```go
// LogPath returns ~/.config/hopbox/run/<host>.log.
func LogPath(hostName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox", "run", hostName+".log"), nil
}
```

**Step 7: Verify compilation**

Run: `go build ./cmd/hop/...`
Expected: BUILD OK

**Step 8: Commit**

```bash
git add cmd/hop/up.go internal/daemon/client.go
git commit -m "feat: refactor hop up to launch daemon by default, --foreground for old behavior"
```

---

### Task 7: Rewrite `hop down`

**Files:**
- Modify: `cmd/hop/down.go` — full rewrite

**Step 1: Write the new implementation**

```go
// cmd/hop/down.go
package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/daemon"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// DownCmd tears down the tunnel by stopping the daemon.
type DownCmd struct{}

func (c *DownCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	client, err := daemon.NewClient(hostName)
	if err != nil {
		return err
	}

	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("no tunnel running for %q", hostName)
	}

	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel %s stopped", hostName)))
	return nil
}
```

**Step 2: Update Down command signature in main.go**

The current `DownCmd.Run()` has no `globals` parameter. The new one needs it for `resolveHost()`. Check if `cmd/hop/main.go` line 23 passes globals — Kong passes the parent struct to `Run(globals *CLI)` automatically when the method accepts it.

Current signature: `func (c *DownCmd) Run() error`
New signature: `func (c *DownCmd) Run(globals *CLI) error`

Kong calls `Run(globals)` automatically since the `DownCmd` is a field of `CLI`. No changes needed to main.go beyond the signature change (which is in down.go).

Also update the help text in main.go:
```go
Down    DownCmd     `cmd:"" help:"Tear down tunnel."`
```

**Step 3: Verify compilation**

Run: `go build ./cmd/hop/...`
Expected: BUILD OK

**Step 4: Commit**

```bash
git add cmd/hop/down.go cmd/hop/main.go
git commit -m "feat: rewrite hop down to stop daemon via socket"
```

---

### Task 8: Stale socket cleanup

**Files:**
- Modify: `internal/tunnel/state.go` — add socket cleanup to `LoadState()`
- Modify: `internal/daemon/client.go` — add `RemoveStaleSocket()`
- Test: `internal/daemon/client_test.go` — add stale socket test

When `LoadState()` detects a dead PID and removes the state file, it should also clean up the corresponding socket file.

**Step 1: Write the failing test**

Add to `internal/daemon/client_test.go`:

```go
func TestRemoveStaleSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "stale.sock")

	// Create a stale socket file (just a regular file pretending).
	if err := os.WriteFile(sockPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	RemoveStaleSocket(sockPath)

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("stale socket was not removed")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/... -run TestRemoveStaleSocket -v`
Expected: FAIL — `RemoveStaleSocket` doesn't exist

**Step 3: Implement**

Add to `internal/daemon/client.go`:
```go
// RemoveStaleSocket removes a socket file if it exists.
func RemoveStaleSocket(sockPath string) {
	_ = os.Remove(sockPath)
}
```

Update `internal/tunnel/state.go` in `LoadState()` — after removing a stale state file (line 70), also remove the socket:

```go
if state.PID > 0 && !pidAlive(state.PID) {
	_ = os.Remove(path)
	// Also remove stale daemon socket.
	sockPath := filepath.Join(dir, hostName+".sock")
	_ = os.Remove(sockPath)
	return nil, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/... ./internal/tunnel/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/client.go internal/daemon/client_test.go internal/tunnel/state.go
git commit -m "fix: clean up stale daemon socket when PID is dead"
```

---

### Task 9: Integration test — full client/server round-trip

**Files:**
- Modify: `internal/daemon/server_test.go` — add integration test

This test verifies the full IPC round-trip: client → server → handler → response, including readiness polling and shutdown.

**Step 1: Write the test**

Add to `internal/daemon/server_test.go`:

```go
func TestFullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "integration.sock")

	shutdownCh := make(chan struct{})
	handler := &MockHandler{
		status:     &DaemonStatus{PID: os.Getpid(), Connected: true, Interface: "utun7"},
		shutdownCh: shutdownCh,
	}

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	client := &Client{SocketPath: sockPath}

	// WaitForReady should succeed immediately.
	if err := client.WaitForReady(time.Second); err != nil {
		t.Fatalf("WaitForReady: %v", err)
	}

	// IsRunning should be true.
	if !client.IsRunning() {
		t.Error("IsRunning = false, want true")
	}

	// Status should work.
	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", status.PID, os.Getpid())
	}
	if status.Interface != "utun7" {
		t.Errorf("Interface = %q, want %q", status.Interface, "utun7")
	}

	// Shutdown should work.
	if err := client.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-shutdownCh:
	case <-time.After(time.Second):
		t.Fatal("shutdown handler not called")
	}
}
```

**Step 2: Run test**

Run: `go test ./internal/daemon/... -run TestFullRoundTrip -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/server_test.go
git commit -m "test: add full client/server round-trip integration test"
```

---

### Task 10: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md` — check off completed items

**Step 1: Update roadmap**

Change these lines in `ROADMAP.md`:
```
- [ ] `hop up` as background daemon — refactor from foreground process to background service
- [ ] `hop down` — proper teardown command that signals the background `hop up` process
```
to:
```
- [x] `hop up` as background daemon — refactor from foreground process to background service
- [x] `hop down` — proper teardown command that signals the background `hop up` process
```

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark daemon mode and hop down as complete in roadmap"
```

---

### Task 11: Manual verification

Verify the full flow works end-to-end:

**Step 1: Build**

Run: `make build`

**Step 2: Test daemon lifecycle**

```bash
# Start daemon directly
hop daemon start <host>
# In another terminal:
hop daemon status <host>   # Should show PID, connected, etc.
hop daemon stop <host>     # Should stop cleanly
```

**Step 3: Test hop up daemon mode**

```bash
hop up                     # Should launch daemon, run TUI, exit
hop status                 # Should show running tunnel
hop up                     # Re-attach: should skip daemon, re-run TUI
hop down                   # Should stop daemon
hop status                 # Should show no tunnel
```

**Step 4: Test foreground mode**

```bash
hop up --foreground        # Should block like before
# Ctrl-C                   # Should clean up
```

**Step 5: Test edge cases**

```bash
# Double daemon start
hop daemon start <host> &
hop daemon start <host>    # Should error "already running"

# Stale cleanup
hop up --foreground &
kill -9 $!                 # Kill without cleanup
hop up                     # Should detect stale, clean up, start fresh
```
