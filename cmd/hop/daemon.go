package main

import (
	"fmt"
	"os"
	"time"

	"github.com/hopboxdev/hopbox/internal/daemon"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// DaemonCmd manages the tunnel daemon.
type DaemonCmd struct {
	Start  DaemonStartCmd  `cmd:"" help:"Start tunnel daemon for a host."`
	Stop   DaemonStopCmd   `cmd:"" help:"Stop tunnel daemon for a host."`
	Status DaemonStatusCmd `cmd:"" help:"Show daemon status for a host."`
}

// DaemonStartCmd starts the daemon process. Runs in foreground (intended to be
// launched by hop up as a detached child, or directly by power users).
type DaemonStartCmd struct {
	Host      string `arg:"" help:"Host name to start daemon for."`
	Workspace string `help:"Path to hopbox.yaml for bridge configuration." type:"existingfile"`
}

func (c *DaemonStartCmd) Run() error {
	cfg, err := hostconfig.Load(c.Host)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", c.Host, err)
	}

	// Check if daemon is already running.
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	if status, err := client.Status(); err == nil {
		return fmt.Errorf("daemon already running for %q (PID %d)", c.Host, status.PID)
	}

	tunCfg, err := cfg.ToTunnelConfig()
	if err != nil {
		return fmt.Errorf("convert tunnel config: %w", err)
	}

	// Load manifest for bridge config.
	var ws *manifest.Workspace
	if c.Workspace != "" {
		ws, err = manifest.Parse(c.Workspace)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	return daemon.Run(daemon.Config{
		HostName: c.Host,
		TunCfg:   tunCfg,
		Manifest: ws,
	})
}

// DaemonStopCmd sends a shutdown command to a running daemon.
type DaemonStopCmd struct {
	Host string `arg:"" help:"Host name to stop daemon for."`
}

func (c *DaemonStopCmd) Run() error {
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	if err := client.Shutdown(); err != nil {
		return fmt.Errorf("no tunnel running for %q", c.Host)
	}
	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel %s stopped", c.Host)))
	return nil
}

// DaemonStatusCmd queries a running daemon for its current state.
type DaemonStatusCmd struct {
	Host string `arg:"" help:"Host name to query."`
}

func (c *DaemonStatusCmd) Run() error {
	client, err := daemon.NewClient(c.Host)
	if err != nil {
		return err
	}
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("no daemon running for %q", c.Host)
	}

	connStr := "no"
	if status.Connected {
		connStr = "yes"
	}
	fmt.Printf("PID:          %d\n", status.PID)
	fmt.Printf("Interface:    %s\n", status.Interface)
	fmt.Printf("Connected:    %s\n", connStr)
	if !status.LastHealthy.IsZero() {
		fmt.Printf("Last healthy: %s ago\n", time.Since(status.LastHealthy).Round(time.Second))
	}
	if !status.StartedAt.IsZero() {
		fmt.Printf("Uptime:       %s\n", time.Since(status.StartedAt).Round(time.Second))
	}
	if len(status.Bridges) > 0 {
		fmt.Printf("Bridges:      %v\n", status.Bridges)
	}
	_, _ = fmt.Fprintln(os.Stderr)
	return nil
}
