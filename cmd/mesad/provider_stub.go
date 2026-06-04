//go:build !docker

package main

import (
	"fmt"

	"github.com/mesadev/mesa/internal/core/ports"
)

// Without the `docker` build tag, mesad has no compute provider. This keeps the
// default build SDK-free; real deployments build with `-tags docker`.
func newCompute(string) (ports.Compute, error) {
	return nil, fmt.Errorf("mesad built without a compute provider; rebuild with -tags docker")
}
