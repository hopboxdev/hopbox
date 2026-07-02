//go:build docker

package main

import (
	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
)

func newDocker(_ cfg, advertise, metaPort string) (ports.Compute, error) {
	// Boxes are anonymous, so isolate them by default: a dedicated bridge + the
	// daemon's egress fence (allowing the agent + metadata ports). Secure by
	// default, no flags, no scripts.
	return dockerprov.New(advertise,
		dockerprov.WithNetwork("hopboxd-net"),
		dockerprov.WithMetaPort(metaPort))
}
