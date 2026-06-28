//go:build !docker && !k8s

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func newDocker(config.Config) (ports.Compute, error) {
	return nil, fmt.Errorf("hopboxd built without the docker backend; rebuild with -tags docker")
}
