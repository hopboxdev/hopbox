package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// SetupCmd bootstraps a new remote host.
type SetupCmd struct {
	Name   string `arg:"" help:"Name for this host."`
	Addr   string `short:"a" required:"" help:"Remote SSH host IP or hostname."`
	User   string `short:"u" default:"root" help:"SSH username."`
	SSHKey string `short:"k" help:"Path to SSH private key."`
	Port   int    `short:"p" default:"22" help:"SSH port."`
}

func (c *SetupCmd) Run() error {
	opts := setup.Options{
		Name:       c.Name,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
		OnStep: func(msg string) {
			fmt.Println(ui.StepOK(msg))
		},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if _, err := setup.Bootstrap(ctx, opts, os.Stdout); err != nil {
		return err
	}

	// Auto-set as default host if no default is configured yet.
	if cfg, err := hostconfig.LoadGlobalConfig(); err == nil && cfg.DefaultHost == "" {
		if err := hostconfig.SetDefaultHost(c.Name); err == nil {
			fmt.Printf("Default host set to %q.\n", c.Name)
		}
	}

	// Install privileged helper if not already present.
	if !helper.IsInstalled() {
		fmt.Println("\nHopbox needs to install a system helper for tunnel networking.")
		fmt.Print("This requires sudo. Proceed? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			helperBin, err := findHelperBinary()
			if err != nil {
				return fmt.Errorf("find helper binary: %w", err)
			}
			cmd := exec.Command("sudo", helperBin, "--install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("install helper: %w", err)
			}
			fmt.Println("Helper installed.")
		} else {
			fmt.Println("Skipped helper installation. hop up will not work without it.")
		}
	}

	return nil
}

// findHelperBinary looks for hop-helper next to the hop binary or in $PATH.
func findHelperBinary() (string, error) {
	// Check next to the current executable.
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "hop-helper")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Check $PATH.
	path, err := exec.LookPath("hop-helper")
	if err == nil {
		return path, nil
	}

	return "", fmt.Errorf("hop-helper not found next to hop binary or in $PATH")
}
