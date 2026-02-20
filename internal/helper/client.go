package helper

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client communicates with the helper daemon over a Unix socket.
type Client struct {
	SocketPath string
}

// NewClient returns a Client using the default socket path.
func NewClient() *Client {
	return &Client{SocketPath: SocketPath}
}

func (c *Client) send(req Request) error {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to helper at %s: %w", c.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("helper: %s", resp.Error)
	}
	return nil
}

// ConfigureTUN asks the helper to assign an IP and add a route for the interface.
func (c *Client) ConfigureTUN(iface, localIP, peerIP string) error {
	return c.send(Request{
		Action:    ActionConfigureTUN,
		Interface: iface,
		LocalIP:   localIP,
		PeerIP:    peerIP,
	})
}

// CleanupTUN asks the helper to remove routes for the tunnel.
func (c *Client) CleanupTUN(iface string) error {
	return c.send(Request{
		Action:    ActionCleanupTUN,
		Interface: iface,
	})
}

// AddHost asks the helper to add an /etc/hosts entry.
func (c *Client) AddHost(ip, hostname string) error {
	return c.send(Request{
		Action:   ActionAddHost,
		IP:       ip,
		Hostname: hostname,
	})
}

// RemoveHost asks the helper to remove an /etc/hosts entry.
func (c *Client) RemoveHost(hostname string) error {
	return c.send(Request{
		Action:   ActionRemoveHost,
		Hostname: hostname,
	})
}

// IsReachable returns true if the helper daemon is responding.
func (c *Client) IsReachable() bool {
	conn, err := net.DialTimeout("unix", c.SocketPath, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
