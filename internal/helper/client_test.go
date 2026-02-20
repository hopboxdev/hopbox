package helper

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func startMockHelper(t *testing.T, handler func(Request) Response) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "helper.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				dec := json.NewDecoder(conn)
				enc := json.NewEncoder(conn)
				var req Request
				if dec.Decode(&req) == nil {
					resp := handler(req)
					if err := enc.Encode(resp); err != nil {
						return
					}
				}
			}()
		}
	}()
	return sock
}

func TestClientConfigureTUN(t *testing.T) {
	var got Request
	sock := startMockHelper(t, func(req Request) Response {
		got = req
		return Response{OK: true}
	})

	c := &Client{SocketPath: sock}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionConfigureTUN || got.Interface != "utun5" {
		t.Errorf("unexpected request: %+v", got)
	}
}

func TestClientAddHost(t *testing.T) {
	var got Request
	sock := startMockHelper(t, func(req Request) Response {
		got = req
		return Response{OK: true}
	})

	c := &Client{SocketPath: sock}
	err := c.AddHost("10.10.0.2", "mybox.hop")
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionAddHost || got.Hostname != "mybox.hop" {
		t.Errorf("unexpected request: %+v", got)
	}
}

func TestClientErrorResponse(t *testing.T) {
	sock := startMockHelper(t, func(req Request) Response {
		return Response{OK: false, Error: "not running as root"}
	})

	c := &Client{SocketPath: sock}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "helper: not running as root" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClientSocketNotFound(t *testing.T) {
	c := &Client{SocketPath: "/nonexistent/helper.sock"}
	err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIsReachable(t *testing.T) {
	// Non-existent socket → not reachable.
	c := &Client{SocketPath: "/nonexistent/helper.sock"}
	if c.IsReachable() {
		t.Error("expected not reachable")
	}

	// Create a mock helper → reachable.
	sock := startMockHelper(t, func(req Request) Response {
		return Response{OK: true}
	})
	c2 := &Client{SocketPath: sock}
	if !c2.IsReachable() {
		t.Error("expected reachable")
	}
}
