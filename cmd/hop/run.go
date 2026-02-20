package main

import "github.com/hopboxdev/hopbox/internal/rpcclient"

// RunCmd executes a named script from the manifest.
type RunCmd struct {
	Script string `arg:"" help:"Script name from hopbox.yaml."`
}

func (c *RunCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CallAndPrint(hostName, "run.script", map[string]string{"name": c.Script})
}
