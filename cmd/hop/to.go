package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
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

	_ = ctx
	return fmt.Errorf("rest not yet implemented")
}
