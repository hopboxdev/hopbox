package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/version"
)

// UpgradeCmd uploads a new hop-agent binary to the remote host and restarts
// the service without touching WireGuard keys or re-running bootstrap.
type UpgradeCmd struct{}

func (c *UpgradeCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	// Warn if the tunnel is currently running — agent will restart mid-session.
	if state, err := tunnel.LoadState(hostName); err == nil && state != nil {
		fmt.Fprintf(os.Stderr, "Warning: tunnel is running (PID %d). The agent will restart; run 'hop down && hop up' to reconnect.\n", state.PID)
	}

	return setup.UpgradeAgent(context.Background(), cfg, os.Stdout, version.Version)
}
