//go:build docker && !k8s

package main

import (
	"github.com/mesadev/mesa/internal/core/ports"
	dockerprov "github.com/mesadev/mesa/providers/compute/docker"
	"github.com/mesadev/mesa/providers/storage/localfs"
)

func newCompute(advertise string) (ports.Compute, error) { return dockerprov.New(advertise) }
func newStorage() ports.Storage                          { return localfs.New("/var/lib/mesa/homes") }
