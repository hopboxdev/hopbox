//go:build docker

package main

import (
	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
)

func newCompute(advertise string) (ports.Compute, error) {
	// Boxes are anonymous, so isolate them by default: a dedicated bridge + the
	// daemon's egress fence. Secure by default, no flags, no scripts.
	return dockerprov.New(advertise, dockerprov.WithNetwork("boxd-net"))
}
