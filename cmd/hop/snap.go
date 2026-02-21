package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/ui"
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
	result, err := rpcclient.Call(hostName, "snap.list", nil)
	if err != nil {
		return err
	}
	var snaps []struct {
		ShortID string    `json:"short_id"`
		Time    time.Time `json:"time"`
	}
	if err := json.Unmarshal(result, &snaps); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(snaps) == 0 {
		fmt.Println(ui.Section("Snapshots", "No snapshots.", ui.MaxWidth))
		return nil
	}

	headers := []string{"ID", "CREATED"}
	var rows [][]string
	for _, s := range snaps {
		age := formatDuration(time.Since(s.Time)) + " ago"
		rows = append(rows, []string{s.ShortID, age})
	}
	fmt.Println(ui.Section("Snapshots", ui.Table(headers, rows), ui.MaxWidth))
	return nil
}
