//go:build firecracker

package microvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// fcClient talks to a Firecracker instance over its API unix socket. Booting via
// the API (rather than --no-api --config-file) is what makes snapshots possible
// (F4): the same fcConfig sections become PUT bodies.
type fcClient struct {
	http *http.Client
}

func newFCClient(sock string) *fcClient {
	return &fcClient{http: &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}}
}

func (c *fcClient) put(path string, body any) error   { return c.do(http.MethodPut, path, body) }
func (c *fcClient) patch(path string, body any) error { return c.do(http.MethodPatch, path, body) }

func (c *fcClient) do(method, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, "http://unix"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fc %s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(rb))
	}
	return nil
}

// boot configures the machine from cfg and starts it.
func (c *fcClient) boot(cfg fcConfig) error {
	if err := c.put("/boot-source", cfg.BootSource); err != nil {
		return err
	}
	for _, d := range cfg.Drives {
		if err := c.put("/drives/"+d.DriveID, d); err != nil {
			return err
		}
	}
	if err := c.put("/machine-config", cfg.MachineConfig); err != nil {
		return err
	}
	for _, n := range cfg.NetworkIfaces {
		if err := c.put("/network-interfaces/"+n.IfaceID, n); err != nil {
			return err
		}
	}
	return c.put("/actions", map[string]string{"action_type": "InstanceStart"})
}

// pause moves the VM to the Paused state (a precondition for snapshotting). VM
// state changes are PATCH /vm in the Firecracker API.
func (c *fcClient) pause() error {
	return c.patch("/vm", map[string]string{"state": "Paused"})
}

// snapshot writes a full snapshot (VM state + guest memory) to the given paths.
func (c *fcClient) snapshot(statePath, memPath string) error {
	return c.put("/snapshot/create", map[string]any{
		"snapshot_type": "Full",
		"snapshot_path": statePath,
		"mem_file_path": memPath,
	})
}

// loadSnapshot restores a VM from disk (into a fresh firecracker), optionally
// resuming it immediately.
func (c *fcClient) loadSnapshot(statePath, memPath string, resume bool) error {
	return c.put("/snapshot/load", map[string]any{
		"snapshot_path": statePath,
		"mem_file_path": memPath,
		"resume_vm":     resume,
	})
}

// waitForSocket blocks until firecracker has created its API socket.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("microvm: firecracker api socket %s did not appear", path)
}
