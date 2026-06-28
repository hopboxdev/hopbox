//go:build docker && !k8s

package main

import (
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
)

func newDocker(cfg config.Config) (ports.Compute, error) {
	return dockerprov.New(cfg.AgentAdvertise, dockerprov.WithNetwork(cfg.ComputeNetwork))
}
