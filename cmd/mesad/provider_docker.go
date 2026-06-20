//go:build docker && !k8s

package main

import (
	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
	dockerprov "github.com/mesadev/mesa/providers/compute/docker"
)

func newCompute(cfg config.Config) (ports.Compute, error) {
	return dockerprov.New(cfg.AgentAdvertise)
}
