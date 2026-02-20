package main

import (
	"os"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
)

// LogsCmd streams service logs.
type LogsCmd struct {
	Service string `arg:"" optional:"" help:"Service name (default: all)."`
}

func (c *LogsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CopyTo(hostName, "logs.stream", map[string]string{"name": c.Service}, os.Stdout)
}
