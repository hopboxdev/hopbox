//go:build !docker && !k8s

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

func newCompute(string) (ports.Compute, error) {
	return nil, fmt.Errorf("hopbox-provider built without a compute provider; rebuild with -tags docker")
}
func newStorage() ports.Storage { return localfs.New("/var/lib/hopbox/homes") }
