//go:build !docker

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func newDocker(cfg, string, string) (ports.Compute, error) {
	return nil, fmt.Errorf("boxd built without docker; rebuild with -tags docker")
}
