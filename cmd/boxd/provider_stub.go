//go:build !docker

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Without -tags docker there is no compute backend; boxd needs one to run.
func newCompute(string, string) (ports.Compute, error) {
	return nil, fmt.Errorf("boxd built without a compute provider; rebuild with -tags docker")
}
