package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// RotateCmd rotates WireGuard keys for a host without full re-setup.
type RotateCmd struct{}

func (c *RotateCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	// Warn if the tunnel is currently running.
	if state, err := tunnel.LoadState(hostName); err == nil && state != nil {
		fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("tunnel is running (PID %d). Run 'hop down && hop up' after rotation to apply new keys", state.PID)))
	}

	return setup.RotateKeys(context.Background(), cfg, os.Stdout)
}
