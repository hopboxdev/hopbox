package main

import (
	"encoding/json"
	"fmt"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// ServicesCmd manages workspace services.
type ServicesCmd struct {
	Ls      ServicesLsCmd      `cmd:"" name:"ls" help:"List services."`
	Restart ServicesRestartCmd `cmd:"" name:"restart" help:"Restart a service."`
	Stop    ServicesStopCmd    `cmd:"" name:"stop" help:"Stop a service."`
}

// ServicesLsCmd lists services.
type ServicesLsCmd struct{}

func (c *ServicesLsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	result, err := rpcclient.Call(hostName, "services.list", nil)
	if err != nil {
		return err
	}
	var svcs []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Running bool   `json:"running"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(result, &svcs); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(svcs) == 0 {
		fmt.Println(ui.Section("Services", "No services.", ui.MaxWidth))
		return nil
	}

	headers := []string{"NAME", "STATUS", "TYPE"}
	var rows [][]string
	for _, s := range svcs {
		dot := ui.Dot(ui.StateStopped)
		status := "stopped"
		if s.Running {
			dot = ui.Dot(ui.StateConnected)
			status = "running"
		}
		if s.Error != "" {
			dot = ui.Dot(ui.StateDisconnected)
			status = "error: " + s.Error
		}
		rows = append(rows, []string{dot + " " + s.Name, status, s.Type})
	}
	fmt.Println(ui.Section("Services", ui.Table(headers, rows), ui.MaxWidth))
	return nil
}

// ServicesRestartCmd restarts a named service.
type ServicesRestartCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesRestartCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CallAndPrint(hostName, "services.restart", map[string]string{"name": c.Name})
}

// ServicesStopCmd stops a named service.
type ServicesStopCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesStopCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CallAndPrint(hostName, "services.stop", map[string]string{"name": c.Name})
}
