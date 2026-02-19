//go:build !linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// ServerTunnel is the non-Linux fallback: uses netstack (userspace) just like
// the client. This enables macOS development and testing without a Linux VPS.
type ServerTunnel struct {
	cfg      Config
	dev      *device.Device
	tnet     *netstack.Net
	ready    chan struct{}
	stopOnce sync.Once
}

// NewServerTunnel creates a new (not yet started) server tunnel.
func NewServerTunnel(cfg Config) *ServerTunnel {
	return &ServerTunnel{cfg: cfg, ready: make(chan struct{})}
}

// Ready returns a channel that is closed once the WireGuard device is up.
func (t *ServerTunnel) Ready() <-chan struct{} {
	return t.ready
}

// Start brings up the userspace WireGuard device using netstack.
// Blocks until ctx is cancelled.
func (t *ServerTunnel) Start(ctx context.Context) error {
	localPrefix, err := netip.ParsePrefix(t.cfg.LocalIP)
	if err != nil {
		return fmt.Errorf("parse local IP %q: %w", t.cfg.LocalIP, err)
	}

	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{localPrefix.Addr()},
		nil,
		t.cfg.MTU,
	)
	if err != nil {
		return fmt.Errorf("create netstack TUN: %w", err)
	}

	logger := device.NewLogger(device.LogLevelSilent, "")
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	ipcConf := BuildServerIPC(t.cfg)
	if err := dev.IpcSet(ipcConf); err != nil {
		dev.Close()
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("device Up: %w", err)
	}

	t.dev = dev
	t.tnet = tnet
	close(t.ready)

	<-ctx.Done()
	t.Stop()
	return nil
}

// Stop tears down the WireGuard device. Safe to call concurrently or more
// than once; the actual close runs exactly once.
func (t *ServerTunnel) Stop() {
	t.stopOnce.Do(func() {
		if t.dev != nil {
			t.dev.Close()
			t.dev = nil
			t.tnet = nil
		}
	})
}

// Status returns current tunnel metrics.
func (t *ServerTunnel) Status() *Status {
	s := &Status{
		LocalIP: t.cfg.LocalIP,
		PeerIP:  t.cfg.PeerIP,
	}
	if t.dev == nil {
		return s
	}
	raw, err := t.dev.IpcGet()
	if err != nil {
		return s
	}
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
		}
	}
	return s
}

// DialContext opens a connection through the WireGuard tunnel via netstack.
// Available on non-Linux for development use.
func (t *ServerTunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if t.tnet == nil {
		return nil, fmt.Errorf("tunnel not started")
	}
	return t.tnet.DialContext(ctx, network, addr)
}
