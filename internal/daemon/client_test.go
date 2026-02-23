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
