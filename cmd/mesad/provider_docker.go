//go:build docker

package main

import (
	"github.com/mesadev/mesa/internal/core/ports"
	dockerprov "github.com/mesadev/mesa/providers/compute/docker"
)

func newCompute(advertise string) (ports.Compute, error) {
	return dockerprov.New(advertise)
}
