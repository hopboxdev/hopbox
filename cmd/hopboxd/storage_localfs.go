//go:build !k8s

package main

import (
	"path/filepath"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

// M1: storage root is derived from the db path's directory: <dir>/homes. The
// microVM backend can't bind-mount a host dir, so it gets block (ext4-image)
// homes attached as a drive; docker keeps directory homes.
func newStorage(cfg config.Config) ports.Storage {
	root := filepath.Join(filepath.Dir(cfg.DBPath), "homes")
	if cfg.ComputeKind == "microvm" {
		return localfs.NewBlock(root, cfg.HomeSizeMB)
	}
	return localfs.New(root)
}
