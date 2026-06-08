//go:build !docker

package main

import (
	"fmt"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/providers/storage/localfs"
)

func newCompute(string) (ports.Compute, error) {
	return nil, fmt.Errorf("mesa-provider built without a compute provider; rebuild with -tags docker")
}
func newStorage() ports.Storage { return localfs.New("/var/lib/mesa/homes") }
