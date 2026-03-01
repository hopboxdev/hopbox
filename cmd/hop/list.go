package main

import (
	"fmt"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// ListCmd shows all hosts and their tunnel status.
type ListCmd struct{}

func (c *ListCmd) Run() error {
	hosts, err := hostconfig.List()
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("No hosts configured. Run 'hop setup <name> -a <ip>' to add one.")
		return nil
	}

	globalCfg, _ := hostconfig.LoadGlobalConfig()
	defaultHost := ""
	if globalCfg != nil {
		defaultHost = globalCfg.DefaultHost
	}

	rows := make([][]string, 0, len(hosts))
	for _, name := range hosts {
		cfg, err := hostconfig.Load(name)
		if err != nil {
			continue
		}

		status := "down"
		workspace := ""
		state, _ := tunnel.LoadState(name)
		if state != nil {
			if state.Connected {
				status = "connected"
			} else {
				status = "disconnected"
			}
			if state.WorkspacePath != "" {
				workspace = state.WorkspacePath
			}
		}

		marker := "  "
		if name == defaultHost {
			marker = "* "
		}

		rows = append(rows, []string{marker + name, cfg.Endpoint, status, workspace})
	}

	fmt.Println(ui.Table(
		[]string{"HOST", "ENDPOINT", "STATUS", "WORKSPACE"},
		rows,
	))

	return nil
}
