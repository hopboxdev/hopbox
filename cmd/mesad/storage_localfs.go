//go:build !k8s

package main

import (
	"path/filepath"

	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/providers/storage/localfs"
)

// M1: storage root is derived from the db path's directory: <dir>/homes.
func newStorage(cfg config.Config) ports.Storage {
	root := filepath.Join(filepath.Dir(cfg.DBPath), "homes")
	return localfs.New(root)
}
