//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// ServerTunnel manages a kernel-mode WireGuard interface on Linux.
// It requires CAP_NET_ADMIN. Traffic routes through the kernel directly;
// DialContext is not available.
type ServerTunnel struct {
	cfg    Config
	dev    *device.Device
	ifName string
	ready  chan struct{}
}

// NewServerTunnel creates a new (not yet started) server tunnel.
func NewServerTunnel(cfg Config) *ServerTunnel {
	return &ServerTunnel{cfg: cfg, ifName: "wg0", ready: make(chan struct{})}
}

// Ready returns a channel that is closed once the WireGuard interface is up
// and the IP address has been assigned. Callers that need to bind on the
// tunnel IP must wait on this before calling net.Listen.
func (t *ServerTunnel) Ready() <-chan struct{} {
	return t.ready
}

// Start brings up the kernel TUN interface and WireGuard device.
// Blocks until ctx is cancelled, then tears down.
func (t *ServerTunnel) Start(ctx context.Context) error {
	tunDev, err := tun.CreateTUN(t.ifName, t.cfg.MTU)
	if err != nil {
		return fmt.Errorf("CreateTUN %q: %w", t.ifName, err)
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

	// Assign IP address to interface
	localPrefix := t.cfg.LocalIP
	if err := run("ip", "addr", "add", localPrefix, "dev", t.ifName); err != nil {
		dev.Close()
		return fmt.Errorf("ip addr add: %w", err)
	}
	if err := run("ip", "link", "set", t.ifName, "up"); err != nil {
		dev.Close()
		return fmt.Errorf("ip link set up: %w", err)
	}

	t.dev = dev
	close(t.ready)

	<-ctx.Done()
	t.Stop()
	return nil
}

// Stop tears down the WireGuard device and interface.
func (t *ServerTunnel) Stop() {
	if t.dev != nil {
		t.dev.Close()
		t.dev = nil
	}
	// Best-effort cleanup of the interface.
	_ = run("ip", "link", "del", t.ifName)
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

// DialContext is not available for the kernel server tunnel.
func (t *ServerTunnel) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	return nil, fmt.Errorf("DialContext not supported on kernel server tunnel; traffic routes through kernel")
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
