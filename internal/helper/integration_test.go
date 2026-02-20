package helper

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelperIntegration verifies the full client → daemon → response flow
// using a mock daemon (no root needed).
func TestHelperIntegration(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	writeHosts(t, hostsPath, "127.0.0.1 localhost\n")

	// Start a mock daemon that handles all actions using real logic
	// but operates on temp files instead of /etc/hosts.
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
				if dec.Decode(&req) != nil {
					return
				}

				var actionErr error
				switch req.Action {
				case ActionAddHost:
					actionErr = AddHostEntry(hostsPath, req.IP, req.Hostname)
				case ActionRemoveHost:
					actionErr = RemoveHostEntry(hostsPath, req.Hostname)
				case ActionConfigureTUN:
					// Skip actual TUN config in tests (requires root).
					if req.Interface == "" || req.LocalIP == "" || req.PeerIP == "" {
						actionErr = fmt.Errorf("missing required fields")
					}
				case ActionCleanupTUN:
					// No-op in tests.
				}

				if actionErr != nil {
					_ = enc.Encode(Response{OK: false, Error: actionErr.Error()})
					return
				}
				_ = enc.Encode(Response{OK: true})
			}()
		}
	}()

	c := &Client{SocketPath: sock}

	// Test: ConfigureTUN
	if err := c.ConfigureTUN("utun5", "10.10.0.1", "10.10.0.2"); err != nil {
		t.Fatalf("ConfigureTUN: %v", err)
	}

	// Test: AddHost
	if err := c.AddHost("10.10.0.2", "mybox.hop"); err != nil {
		t.Fatalf("AddHost: %v", err)
	}

	// Verify hosts file was updated.
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	if !strings.Contains(string(data), "mybox.hop") {
		t.Error("hosts file missing mybox.hop after AddHost")
	}

	// Test: AddHost second entry
	if err := c.AddHost("10.10.0.3", "gaming.hop"); err != nil {
		t.Fatalf("AddHost second: %v", err)
	}

	// Test: RemoveHost
	if err := c.RemoveHost("mybox.hop"); err != nil {
		t.Fatalf("RemoveHost: %v", err)
	}

	// Verify mybox.hop removed but gaming.hop remains.
	data, err = os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	if strings.Contains(string(data), "mybox.hop") {
		t.Error("hosts file still contains mybox.hop after RemoveHost")
	}
	if !strings.Contains(string(data), "gaming.hop") {
		t.Error("hosts file missing gaming.hop after RemoveHost of mybox.hop")
	}

	// Test: CleanupTUN
	if err := c.CleanupTUN("utun5"); err != nil {
		t.Fatalf("CleanupTUN: %v", err)
	}

	// Test: IsReachable
	if !c.IsReachable() {
		t.Error("expected helper to be reachable")
	}

	// Shut down and verify not reachable.
	_ = ln.Close()
	c2 := &Client{SocketPath: sock}
	if c2.IsReachable() {
		t.Error("expected helper to be unreachable after close")
	}
}
