//go:build !firecracker

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func newMicrovm(cfg, string, string) (ports.Compute, error) {
	return nil, fmt.Errorf("hopboxd built without microvm; rebuild with -tags firecracker")
}
