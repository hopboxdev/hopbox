package tunnel

import (
	"context"
	"net"
	"strconv"
	"strings"
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

// parseIpcOutput fills s from the key=value output of device.IpcGet().
// It is shared by all tunnel Status() implementations.
func parseIpcOutput(raw string, s *Status) {
	s.IsUp = true
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "last_handshake_time_sec":
			sec, err := strconv.ParseInt(v, 10, 64)
			if err == nil && sec > 0 {
				s.LastHandshake = time.Unix(sec, 0)
			}
		case "tx_bytes":
			n, _ := strconv.ParseInt(v, 10, 64)
			s.BytesSent = n
		case "rx_bytes":
			n, _ := strconv.ParseInt(v, 10, 64)
			s.BytesReceived = n
		case "endpoint":
			s.Endpoint = v
		}
	}
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
