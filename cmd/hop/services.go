package main

import (
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
	return rpcclient.CallAndPrint(hostName, "services.list", nil)
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
