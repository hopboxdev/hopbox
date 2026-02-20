//go:build darwin

package tunnel

import (
	"context"
	"fmt"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// KernelTunnel is a kernel-mode WireGuard tunnel for macOS.
// It creates a utun device that is visible system-wide — any process can
// connect to the peer IP without DialContext.
type KernelTunnel struct {
	cfg      Config
	dev      *device.Device
	ifName   string
	ready    chan struct{}
	stopOnce sync.Once
}

// NewKernelTunnel creates a new (not yet started) kernel tunnel.
func NewKernelTunnel(cfg Config) *KernelTunnel {
	return &KernelTunnel{cfg: cfg, ready: make(chan struct{})}
}

// Start brings up the kernel TUN device and WireGuard protocol.
// Blocks until ctx is cancelled, then tears down.
func (t *KernelTunnel) Start(ctx context.Context) error {
	tunDev, err := tun.CreateTUN("utun", t.cfg.MTU)
	if err != nil {
		return fmt.Errorf("CreateTUN: %w", err)
	}

	name, err := tunDev.Name()
	if err != nil {
		_ = tunDev.Close()
		return fmt.Errorf("get TUN name: %w", err)
	}
	t.ifName = name

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
	close(t.ready)

	<-ctx.Done()
	t.Stop()
	return nil
}

// Stop tears down the WireGuard device. The utun interface is destroyed
// automatically when the fd is closed.
func (t *KernelTunnel) Stop() {
	t.stopOnce.Do(func() {
		if t.dev != nil {
			t.dev.Close()
			t.dev = nil
		}
	})
}

// Ready returns a channel that closes once the TUN device is up.
func (t *KernelTunnel) Ready() <-chan struct{} {
	return t.ready
}

// InterfaceName returns the utun interface name (e.g. "utun5").
// Only valid after Ready() has closed.
func (t *KernelTunnel) InterfaceName() string {
	return t.ifName
}

// Status returns current tunnel metrics.
func (t *KernelTunnel) Status() *Status {
	s := &Status{LocalIP: t.cfg.LocalIP, PeerIP: t.cfg.PeerIP}
	if t.dev == nil {
		return s
	}
	raw, err := t.dev.IpcGet()
	if err != nil {
		return s
	}
	parseIpcOutput(raw, s)
	return s
}
