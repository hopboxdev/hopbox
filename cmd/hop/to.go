package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/setup"
)

// ToCmd migrates the workspace to a new host.
type ToCmd struct {
	Target string `arg:"" help:"Name for the target host."`
	Addr   string `short:"a" required:"" help:"Remote SSH host IP or hostname."`
	User   string `short:"u" default:"root" help:"SSH username."`
	SSHKey string `short:"k" help:"Path to SSH private key."`
	Port   int    `short:"p" default:"22" help:"SSH port."`
}

func (c *ToCmd) Run(globals *CLI) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sourceHost, err := resolveHost(globals)
	if err != nil {
		return fmt.Errorf("source host: %w", err)
	}
	if c.Target == sourceHost {
		return fmt.Errorf("target host must differ from source host")
	}

	// Show confirmation prompt before any work begins.
	fmt.Printf("Migrate workspace from %s → %s (%s)?\n", sourceHost, c.Target, c.Addr)
	fmt.Println("  1. Create snapshot on", sourceHost)
	fmt.Println("  2. Bootstrap", c.Target, "via SSH")
	fmt.Println("  3. Restore snapshot on", c.Target)
	fmt.Println("  4. Set", c.Target, "as default host")
	fmt.Print("\nProceed? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
		fmt.Println("Aborted.")
		return nil
	}

	// Step 1/4: Snapshot source.
	fmt.Printf("\nStep 1/4  Snapshot  creating snapshot on %s...\n", sourceHost)
	snapResult, err := rpcclient.Call(sourceHost, "snap.create", nil)
	if err != nil {
		return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
	}
	var snap struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal(snapResult, &snap); err != nil || snap.SnapshotID == "" {
		return fmt.Errorf("could not parse snapshot ID from response: %s", string(snapResult))
	}
	fmt.Printf("            snapshot %s created.\n", snap.SnapshotID)

	// Step 2/4: Bootstrap target.
	fmt.Printf("\nStep 2/4  Bootstrap  setting up %s...\n", c.Target)
	targetCfg, err := setup.Bootstrap(ctx, setup.Options{
		Name:       c.Target,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
	}, os.Stdout)
	if err != nil {
		return fmt.Errorf("bootstrap %s: %w", c.Target, err)
	}
	fmt.Printf("            %s bootstrapped.\n", c.Target)

	_ = targetCfg
	return fmt.Errorf("steps 3-4 not yet implemented")
}
