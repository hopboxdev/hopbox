package tunnel

import (
	"fmt"
	"strings"
)

// BuildClientIPC produces the IpcSet string for a WireGuard client device.
// Keys must be hex-encoded (64 hex chars = 32 bytes).
// Format: newline-separated key=value pairs; peer section begins with public_key=.
func BuildClientIPC(cfg Config) string {
	var b strings.Builder

	// Interface section
	fmt.Fprintf(&b, "private_key=%s\n", cfg.PrivateKey)
	if cfg.ListenPort > 0 {
		fmt.Fprintf(&b, "listen_port=%d\n", cfg.ListenPort)
	}

	// Peer section
	fmt.Fprintf(&b, "public_key=%s\n", cfg.PeerPublicKey)
	fmt.Fprintf(&b, "allowed_ip=%s\n", cfg.PeerIP)
	if cfg.Endpoint != "" {
		fmt.Fprintf(&b, "endpoint=%s\n", cfg.Endpoint)
	}
	if cfg.PersistentKeepalive > 0 {
		secs := int(cfg.PersistentKeepalive.Seconds())
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", secs)
	}

	return b.String()
}

// BuildServerIPC produces the IpcSet string for a WireGuard server device.
func BuildServerIPC(cfg Config) string {
	var b strings.Builder

	// Interface section
	fmt.Fprintf(&b, "private_key=%s\n", cfg.PrivateKey)
	fmt.Fprintf(&b, "listen_port=%d\n", cfg.ListenPort)

	// Peer section
	fmt.Fprintf(&b, "public_key=%s\n", cfg.PeerPublicKey)
	fmt.Fprintf(&b, "allowed_ip=%s\n", cfg.PeerIP)

	return b.String()
}
