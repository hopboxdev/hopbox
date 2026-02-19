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

// ClientTunnel is a userspace WireGuard tunnel for the hop client.
// It uses gVisor's netstack so it requires no root privileges.
type ClientTunnel struct {
	cfg      Config
	dev      *device.Device
	tnet     *netstack.Net
	stopOnce sync.Once
}

// NewClientTunnel creates a new (not yet started) client tunnel.
func NewClientTunnel(cfg Config) *ClientTunnel {
	return &ClientTunnel{cfg: cfg}
}

// Start brings up the WireGuard tunnel. It blocks until ctx is cancelled,
// then tears down the device.
func (t *ClientTunnel) Start(ctx context.Context) error {
	localAddr, err := netip.ParsePrefix(t.cfg.LocalIP)
	if err != nil {
		return fmt.Errorf("parse local IP %q: %w", t.cfg.LocalIP, err)
	}

	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{localAddr.Addr()},
		nil, // no DNS servers needed
		t.cfg.MTU,
	)
	if err != nil {
		return fmt.Errorf("create netstack TUN: %w", err)
	}

	logger := device.NewLogger(device.LogLevelSilent, "")
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	ipcConf := BuildClientIPC(t.cfg)
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

	// Wait for context cancellation
	<-ctx.Done()
	t.Stop()
	return nil
}

// Stop tears down the WireGuard device. Safe to call concurrently or more
// than once; the actual close runs exactly once.
func (t *ClientTunnel) Stop() {
	t.stopOnce.Do(func() {
		if t.dev != nil {
			t.dev.Close()
			t.dev = nil
			t.tnet = nil
		}
	})
}

// Status returns current tunnel metrics parsed from IpcGet output.
func (t *ClientTunnel) Status() *Status {
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
		case "endpoint":
			s.Endpoint = v
		}
	}
	return s
}

// DialContext opens a connection through the WireGuard tunnel via netstack.
func (t *ClientTunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if t.tnet == nil {
		return nil, fmt.Errorf("tunnel not started")
	}
	return t.tnet.DialContext(ctx, network, addr)
}
