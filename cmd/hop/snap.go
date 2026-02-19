package main

import (
	"encoding/json"
	"fmt"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
)

// SnapCmd manages workspace snapshots.
type SnapCmd struct {
	Create  SnapCreateCmd  `cmd:"" name:"create" help:"Create a new snapshot." default:"1"`
	Restore SnapRestoreCmd `cmd:"" name:"restore" help:"Restore from a snapshot."`
	Ls      SnapLsCmd      `cmd:"" name:"ls" help:"List snapshots."`
}

// SnapCreateCmd creates a new snapshot.
type SnapCreateCmd struct{}

func (c *SnapCreateCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	result, err := rpcclient.Call(hostName, "snap.create", nil)
	if err != nil {
		return err
	}
	var snap struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal(result, &snap); err == nil && snap.SnapshotID != "" {
		fmt.Printf("Snapshot created: %s\n", snap.SnapshotID)
		return nil
	}
	fmt.Println(string(result))
	return nil
}

// SnapRestoreCmd restores a workspace from a snapshot.
type SnapRestoreCmd struct {
	ID          string `arg:"" help:"Snapshot ID to restore."`
	RestorePath string `help:"Restore root path (default: /)."`
}

func (c *SnapRestoreCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	params := map[string]string{"id": c.ID}
	if c.RestorePath != "" {
		params["restore_path"] = c.RestorePath
	}
	return rpcclient.CallAndPrint(hostName, "snap.restore", params)
}

// SnapLsCmd lists snapshots.
type SnapLsCmd struct{}

func (c *SnapLsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcclient.CallAndPrint(hostName, "snap.list", nil)
}
