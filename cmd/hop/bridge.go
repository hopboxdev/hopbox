package main

import (
	"fmt"
	"strings"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// BridgeCmd manages local-remote bridges.
type BridgeCmd struct {
	Ls      BridgeLsCmd      `cmd:"" name:"ls" help:"List configured bridges."`
	Restart BridgeRestartCmd `cmd:"" name:"restart" help:"Restart a bridge."`
}

// BridgeLsCmd lists bridges from the local manifest.
type BridgeLsCmd struct {
	Workspace string `short:"w" help:"Path to hopbox.yaml." default:"hopbox.yaml"`
}

func (c *BridgeLsCmd) Run() error {
	ws, err := manifest.Parse(c.Workspace)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if len(ws.Bridges) == 0 {
		fmt.Println(ui.Section("Bridges", "No bridges configured.", ui.MaxWidth))
		return nil
	}
	var lines []string
	for _, b := range ws.Bridges {
		lines = append(lines, fmt.Sprintf("%s %s   configured", ui.Dot(ui.StateConnected), b.Type))
	}
	fmt.Println(ui.Section("Bridges", strings.Join(lines, "\n"), ui.MaxWidth))
	return nil
}

// BridgeRestartCmd restarts a bridge (requires tunnel to be up via hop up).
type BridgeRestartCmd struct {
	Type string `arg:"" help:"Bridge type (clipboard, cdp)."`
}

func (c *BridgeRestartCmd) Run() error {
	return fmt.Errorf("bridge restart requires restarting 'hop up': run 'hop down' then 'hop up'")
}
