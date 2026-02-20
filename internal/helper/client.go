package helper

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
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

// CreateTUN asks the helper to create a utun device and return the fd.
// The fd is passed via SCM_RIGHTS over the Unix socket. Returns the
// utun file and the interface name (e.g. "utun5").
func (c *Client) CreateTUN(mtu int) (*os.File, string, error) {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return nil, "", fmt.Errorf("connect to helper at %s: %w", c.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Action: ActionCreateTUN, MTU: mtu}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, "", fmt.Errorf("send request: %w", err)
	}

	// Receive the JSON response + utun fd via SCM_RIGHTS.
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, "", fmt.Errorf("expected UnixConn, got %T", conn)
	}

	buf := make([]byte, 4096)
	oob := make([]byte, unix.CmsgSpace(4)) // space for one fd
	n, oobn, _, _, err := unixConn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		return nil, "", fmt.Errorf("helper: %s", resp.Error)
	}

	// Extract the fd from the control message.
	cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, "", fmt.Errorf("parse control message: %w", err)
	}
	if len(cmsgs) == 0 {
		return nil, "", fmt.Errorf("no fd received from helper")
	}
	fds, err := unix.ParseUnixRights(&cmsgs[0])
	if err != nil {
		return nil, "", fmt.Errorf("parse unix rights: %w", err)
	}
	if len(fds) == 0 {
		return nil, "", fmt.Errorf("no fd in control message")
	}

	tunFile := os.NewFile(uintptr(fds[0]), "utun")
	return tunFile, resp.Interface, nil
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
