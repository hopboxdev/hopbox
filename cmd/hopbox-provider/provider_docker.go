//go:build docker && !k8s

package main

import (
	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

func newCompute(advertise string) (ports.Compute, error) { return dockerprov.New(advertise) }
func newStorage() ports.Storage                          { return localfs.New("/var/lib/hopbox/homes") }
