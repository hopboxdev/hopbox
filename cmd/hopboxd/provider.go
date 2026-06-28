//go:build !k8s

package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// newCompute selects the in-process compute backend at runtime (--compute). Each
// backend is build-tag-gated (docker, firecracker); an unselected one compiles
// to a stub that errors, so any tag combination builds. kubernetes is its own
// exclusive build (provider_k8s.go).
func newCompute(cfg config.Config) (ports.Compute, error) {
	switch cfg.ComputeKind {
	case "microvm":
		return newMicrovm(cfg)
	case "docker", "":
		return newDocker(cfg)
	default:
		return nil, fmt.Errorf("unknown --compute %q (want docker|microvm|kubernetes)", cfg.ComputeKind)
	}
}
