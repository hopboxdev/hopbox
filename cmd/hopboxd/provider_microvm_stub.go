//go:build !firecracker && !k8s

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func newMicrovm(config.Config) (ports.Compute, error) {
	return nil, fmt.Errorf("hopboxd built without the microvm backend; rebuild with -tags firecracker")
}
