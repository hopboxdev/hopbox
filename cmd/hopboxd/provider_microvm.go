//go:build firecracker && !k8s

package main

import (
	"net"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	microvm "github.com/hopboxdev/hopbox/providers/compute/microvm"
)

func newMicrovm(cfg config.Config) (ports.Compute, error) {
	// Firecracker microVMs from the image catalog. The in-box agent reaches the hub
	// over the VM bridge gateway — set --agent-advertise <gateway>:<port> (.1 of the
	// /24). To run beside boxd, set --fc-bridge + --fc-subnet to its own network.
	// The egress fence allows only the agent + metadata ports on the host.
	var allow []string
	if _, p, err := net.SplitHostPort(cfg.AgentListen); err == nil && p != "" {
		allow = append(allow, p)
	}
	if _, p, err := net.SplitHostPort(cfg.MetaAddr); err == nil && p != "" {
		allow = append(allow, p) // box-guest reaches the metadata API
	}
	return microvm.New(cfg.FCBin, cfg.FCKernel, cfg.FCImagesDir, cfg.FCRunDir, allow,
		microvm.NetConfig{Bridge: cfg.FCBridge, Subnet24: cfg.FCSubnet})
}
