//go:build !k8s

package main

import (
	"path/filepath"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/providers/storage/localfs"
)

// M1: storage root is derived from the db path's directory: <dir>/homes.
func newStorage(cfg config.Config) ports.Storage {
	root := filepath.Join(filepath.Dir(cfg.DBPath), "homes")
	return localfs.New(root)
}
