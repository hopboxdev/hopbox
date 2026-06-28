//go:build firecracker && !k8s

package main

import (
	"net"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	microvm "github.com/hopboxdev/hopbox/providers/compute/microvm"
)

func newMicrovm(cfg config.Config) (ports.Compute, error) {
	// Firecracker microVMs from the image catalog. The in-box agent reaches the
	// hub over the VM bridge gateway — set --agent-advertise 10.0.0.1:<port>; the
	// egress fence allows only that port and blocks the rest of the host.
	var allow []string
	if _, p, err := net.SplitHostPort(cfg.AgentListen); err == nil && p != "" {
		allow = append(allow, p)
	}
	return microvm.New(cfg.FCBin, cfg.FCKernel, cfg.FCImagesDir, cfg.FCRunDir, allow)
}
