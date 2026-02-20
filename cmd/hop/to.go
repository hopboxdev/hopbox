package main

import (
	"encoding/json"
	"fmt"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
)

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
