package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/version"
)

// DownCmd tears down the tunnel (no-op in foreground mode).
type DownCmd struct{}

func (c *DownCmd) Run() error {
	fmt.Println("In foreground mode, use Ctrl-C to stop the tunnel.")
	return nil
}

// LogsCmd streams service logs.
type LogsCmd struct {
	Service string `arg:"" optional:"" help:"Service name (default: all)."`
}

func (c *LogsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CopyTo(hostName, "logs.stream", map[string]string{"name": c.Service}, os.Stdout)
}

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

// ToCmd migrates the workspace to a new host.
type ToCmd struct {
	Target string `arg:"" help:"Target host name (must be set up with 'hop setup')."`
}

func (c *ToCmd) Run(globals *CLI) error {
	sourceHost, err := resolveHost(globals)
	if err != nil {
		return fmt.Errorf("source host: %w", err)
	}
	if c.Target == sourceHost {
		return fmt.Errorf("target host must differ from source host")
	}
	if _, err := hostconfig.Load(c.Target); err != nil {
		return fmt.Errorf("target host %q not found: run 'hop setup %s --host <ip>' first", c.Target, c.Target)
	}

	fmt.Printf("Step 1/2: Creating snapshot on %s...\n", sourceHost)
	snapResult, err := rpcclient.Call(sourceHost, "snap.create", nil)
	if err != nil {
		return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
	}
	var snap struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal(snapResult, &snap); err != nil || snap.SnapshotID == "" {
		return fmt.Errorf("could not determine snapshot ID from response: %s", string(snapResult))
	}
	fmt.Printf("Snapshot created: %s\n", snap.SnapshotID)

	fmt.Printf("Step 2/2: Restoring snapshot %s on %s...\n", snap.SnapshotID, c.Target)
	if err := rpcclient.CallAndPrint(c.Target, "snap.restore", map[string]string{"id": snap.SnapshotID}); err != nil {
		return fmt.Errorf("restore on %s: %w", c.Target, err)
	}

	fmt.Printf("\nMigration complete.\n")
	fmt.Printf("Run 'hop up --host %s' to connect to the new host.\n", c.Target)
	return nil
}

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
		fmt.Println("No bridges configured.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "TYPE\tSTATUS\n")
	for _, b := range ws.Bridges {
		_, _ = fmt.Fprintf(tw, "%s\tconfigured\n", b.Type)
	}
	return tw.Flush()
}

// BridgeRestartCmd restarts a bridge (requires tunnel to be up via hop up).
type BridgeRestartCmd struct {
	Type string `arg:"" help:"Bridge type (clipboard, cdp)."`
}

func (c *BridgeRestartCmd) Run() error {
	return fmt.Errorf("bridge restart requires restarting 'hop up': run 'hop down' then 'hop up'")
}

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct{}

func (c *InitCmd) Run() error {
	scaffold := `name: myapp
host: ""

services:
  app:
    type: docker
    image: myapp:latest
    ports: [8080]

bridges:
  - type: clipboard

session:
  manager: zellij
  name: myapp
`
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}
	return os.WriteFile(path, []byte(scaffold), 0644)
}

// VersionCmd prints version info.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("hop %s (commit %s, built %s)\n",
		version.Version, version.Commit, version.Date)
	return nil
}
