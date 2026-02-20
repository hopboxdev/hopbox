package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
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
		fmt.Println("No services.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "NAME\tTYPE\tSTATUS\n")
	for _, s := range svcs {
		status := "stopped"
		if s.Running {
			status = "running"
		}
		if s.Error != "" {
			status = "error: " + s.Error
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Type, status)
	}
	return tw.Flush()
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
