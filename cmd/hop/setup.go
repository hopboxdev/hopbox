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
	"github.com/hopboxdev/hopbox/internal/tui"
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
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// SSH connect with TOFU happens before the TUI (interactive prompt).
	client, capturedKey, err := setup.SSHConnectTOFU(ctx, opts, os.Stdout)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	// Bootstrap via phased TUI runner.
	phases := []tui.Phase{
		{Title: "Bootstrap", Steps: []tui.Step{
			{Title: "Setting up " + c.Name, Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				opts.OnStep = func(msg string) { send(tui.StepEvent{Message: msg}) }
				_, err := setup.BootstrapWithClient(ctx, client, capturedKey, opts, os.Stdout)
				if err != nil {
					return err
				}
				send(tui.StepEvent{Message: fmt.Sprintf("%s ready", c.Name)})
				return nil
			}},
		}},
	}
	if err := tui.RunPhases(ctx, "hop setup", phases); err != nil {
		return err
	}

	// Auto-set as default host if no default is configured yet.
	if gcfg, err := hostconfig.LoadGlobalConfig(); err == nil && gcfg.DefaultHost == "" {
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
