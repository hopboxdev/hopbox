package tunnel

import (
	"context"
	"net"
	"time"
)

// Status holds current tunnel health metrics.
type Status struct {
	IsUp          bool
	LastHandshake time.Time
	BytesSent     int64
	BytesReceived int64
	Endpoint      string
	LocalIP       string
	PeerIP        string
}

// Tunnel is the interface implemented by both client and server tunnel types.
type Tunnel interface {
	// Start brings up the WireGuard tunnel. Blocks until the context is cancelled.
	Start(ctx context.Context) error
	// Stop tears down the tunnel immediately.
	Stop()
	// Status returns current tunnel health information.
	Status() *Status
	// DialContext opens a TCP or UDP connection through the tunnel.
	// Not meaningful on server (kernel TUN) tunnels — returns an error.
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}
