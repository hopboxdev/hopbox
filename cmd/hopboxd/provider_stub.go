//go:build !docker && !k8s

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Without a provider build tag, hopboxd has no compute provider. This keeps the
// default build SDK-free; real deployments build with -tags docker or -tags k8s.
func newCompute(config.Config) (ports.Compute, error) {
	return nil, fmt.Errorf("hopboxd built without a compute provider; rebuild with -tags docker or -tags k8s")
}
