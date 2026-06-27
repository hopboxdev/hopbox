//go:build firecracker

package main

import (
	"github.com/hopboxdev/hopbox/internal/core/ports"
	microvm "github.com/hopboxdev/hopbox/providers/compute/microvm"
)

func newMicrovm(c cfg, _, _ string) (ports.Compute, error) {
	// Firecracker microVMs from the golden agent rootfs. The agent reaches the
	// hub/metadata via the VM gateway (10.0.0.1); the egress fence on the VM
	// subnet is a follow-up.
	return microvm.New(c.fcBin, c.fcKernel, c.fcRootfs, c.fcRunDir)
}
