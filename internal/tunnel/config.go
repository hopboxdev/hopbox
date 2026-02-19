package tunnel

import "time"

const (
	DefaultMTU              = 1420
	DefaultPort             = 51820
	ClientIP                = "10.10.0.1"
	ServerIP                = "10.10.0.2"
	DefaultKeepalive        = 25 * time.Second
	AgentAPIPort            = 4200
)

// Config holds all parameters needed to bring up a WireGuard tunnel endpoint.
type Config struct {
	// PrivateKey is the local private key (hex-encoded).
	PrivateKey string
	// PeerPublicKey is the remote peer's public key (hex-encoded).
	PeerPublicKey string
	// LocalIP is the WireGuard interface IP (CIDR, e.g. "10.10.0.1/24").
	LocalIP string
	// PeerIP is the allowed IP for the peer (CIDR, e.g. "10.10.0.2/32").
	PeerIP string
	// Endpoint is the remote UDP endpoint "host:port" (client only).
	Endpoint string
	// ListenPort is the local UDP listen port (0 = ephemeral).
	ListenPort int
	// MTU for the WireGuard interface.
	MTU int
	// PersistentKeepalive interval (0 = disabled).
	PersistentKeepalive time.Duration
}

// DefaultClientConfig returns a Config with sensible defaults for the client side.
func DefaultClientConfig() Config {
	return Config{
		LocalIP:             ClientIP + "/24",
		PeerIP:              ServerIP + "/32",
		ListenPort:          0,
		MTU:                 DefaultMTU,
		PersistentKeepalive: DefaultKeepalive,
	}
}

// DefaultServerConfig returns a Config with sensible defaults for the server side.
func DefaultServerConfig() Config {
	return Config{
		LocalIP:    ServerIP + "/24",
		PeerIP:     ClientIP + "/32",
		ListenPort: DefaultPort,
		MTU:        DefaultMTU,
	}
}
