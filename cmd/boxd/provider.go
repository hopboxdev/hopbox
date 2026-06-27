package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// newCompute selects the compute backend at runtime (--compute). Each backend is
// build-tag-gated (docker, firecracker); the unselected ones compile to stubs
// that error, so any tag combination builds.
func newCompute(c cfg, advertise, metaPort string) (ports.Compute, error) {
	switch c.compute {
	case "microvm":
		return newMicrovm(c, advertise, metaPort)
	case "docker", "":
		return newDocker(c, advertise, metaPort)
	default:
		return nil, fmt.Errorf("unknown --compute %q (want docker|microvm)", c.compute)
	}
}
