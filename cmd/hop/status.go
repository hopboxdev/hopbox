package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// StatusCmd shows tunnel and workspace health.
type StatusCmd struct{}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	data := fetchDashData(hostName, cfg)
	fmt.Print(renderDashboard(data, 80))
	return nil
}
