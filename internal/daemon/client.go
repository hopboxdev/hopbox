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

// LogPath returns ~/.config/hopbox/run/<host>.log.
func LogPath(hostName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox", "run", hostName+".log"), nil
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

// RemoveStaleSocket removes a socket file if it exists.
func RemoveStaleSocket(sockPath string) {
	_ = os.Remove(sockPath)
}
