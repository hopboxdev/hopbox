//go:build firecracker

package main

import (
	"net"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	microvm "github.com/hopboxdev/hopbox/providers/compute/microvm"
)

func newMicrovm(c cfg, advertise, metaPort string) (ports.Compute, error) {
	// Firecracker microVMs from the golden agent rootfs. The agent reaches the
	// hub + metadata via the VM gateway (10.0.0.1); the egress fence allows only
	// those host ports and blocks the rest (the host's services, LAN, tailnet).
	allow := []string{metaPort}
	if _, p, err := net.SplitHostPort(advertise); err == nil && p != "" {
		allow = append(allow, p)
	}
	return microvm.New(c.fcBin, c.fcKernel, c.fcImagesDir, c.fcRunDir, allow)
}
