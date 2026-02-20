//go:build darwin

package tunnel

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// KernelTunnel is a kernel-mode WireGuard tunnel for macOS.
// It uses a utun device (created by the privileged helper) that is visible
// system-wide — any process can connect to the peer IP without DialContext.
type KernelTunnel struct {
	cfg      Config
	tunFile  *os.File
	dev      *device.Device
	ifName   string
	ready    chan struct{}
	stopOnce sync.Once
}

// NewKernelTunnel creates a new (not yet started) kernel tunnel.
// tunFile is the pre-opened utun fd received from the helper daemon.
// ifName is the interface name (e.g. "utun5").
func NewKernelTunnel(cfg Config, tunFile *os.File, ifName string) *KernelTunnel {
	return &KernelTunnel{cfg: cfg, tunFile: tunFile, ifName: ifName, ready: make(chan struct{})}
}

// Start brings up the WireGuard protocol on the pre-opened TUN device.
// Blocks until ctx is cancelled, then tears down.
func (t *KernelTunnel) Start(ctx context.Context) error {
	// MTU=0 tells CreateTUNFromFile to skip setMTU — the helper already set it.
	tunDev, err := tun.CreateTUNFromFile(t.tunFile, 0)
	if err != nil {
		return fmt.Errorf("CreateTUNFromFile: %w", err)
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
