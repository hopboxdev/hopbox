//go:build !docker && !k8s

package main

import (
	"fmt"

	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
)

// Without a provider build tag, mesad has no compute provider. This keeps the
// default build SDK-free; real deployments build with -tags docker or -tags k8s.
func newCompute(config.Config) (ports.Compute, error) {
	return nil, fmt.Errorf("mesad built without a compute provider; rebuild with -tags docker or -tags k8s")
}
