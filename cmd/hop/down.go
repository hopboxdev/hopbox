package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/daemon"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// DownCmd tears down the tunnel by stopping the daemon.
type DownCmd struct{}

func (c *DownCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	client, err := daemon.NewClient(hostName)
	if err != nil {
		return err
	}

	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("no tunnel running for %q", hostName)
	}

	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel %s stopped", hostName)))
	return nil
}
