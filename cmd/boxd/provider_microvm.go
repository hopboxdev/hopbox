//go:build firecracker

package main

import (
	"net"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	microvm "github.com/hopboxdev/hopbox/providers/compute/microvm"
)

func newMicrovm(c cfg, advertise, metaPort string) (ports.Compute, error) {
	// Firecracker microVMs from the golden agent rootfs. The agent reaches the hub
	// + metadata via the VM bridge gateway; the egress fence allows only those host
	// ports and blocks the rest (the host's services, LAN, tailnet). --fc-bridge /
	// --fc-subnet move the fleet onto its own network to coexist with another daemon.
	allow := []string{metaPort}
	if _, p, err := net.SplitHostPort(advertise); err == nil && p != "" {
		allow = append(allow, p)
	}
	return microvm.New(c.fcBin, c.fcKernel, c.fcImagesDir, c.fcRunDir, allow,
		microvm.NetConfig{Bridge: c.fcBridge, Subnet24: c.fcSubnet})
}
