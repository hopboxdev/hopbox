package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/version"
)

// VersionCmd prints version info.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("hop %s (commit %s, built %s)\n",
		version.Version, version.Commit, version.Date)
	return nil
}
