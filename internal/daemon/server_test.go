package daemon

import (
	"encoding/json"
	"net"
	"os"
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

func TestFullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "integration.sock")

	shutdownCh := make(chan struct{}, 1)
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
