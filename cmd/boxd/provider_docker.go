//go:build docker

package main

import (
	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
)

func newCompute(advertise string) (ports.Compute, error) {
	return dockerprov.New(advertise)
}
