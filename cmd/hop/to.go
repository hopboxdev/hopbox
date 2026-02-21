package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tui"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
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

	// Confirmation prompt (before TUI).
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
	fmt.Println()

	// SSH connect with TOFU for bootstrap target (before TUI).
	bootstrapOpts := setup.Options{
		Name:       c.Target,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
	}
	sshClient, capturedKey, err := setup.SSHConnectTOFU(ctx, bootstrapOpts, os.Stdout)
	if err != nil {
		return fmt.Errorf("SSH connect to %s: %w", c.Target, err)
	}
	defer func() { _ = sshClient.Close() }()

	// Shared state across steps.
	var snapID string
	var targetCfg *hostconfig.HostConfig

	steps := []tui.Step{
		// Step 1: Snapshot source.
		{Title: fmt.Sprintf("Creating snapshot on %s", sourceHost), Run: func(ctx context.Context, sub func(string)) error {
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
			snapID = snap.SnapshotID
			sub(fmt.Sprintf("Snapshot %s created", snapID))
			return nil
		}},

		// Step 2: Bootstrap target.
		{Title: fmt.Sprintf("Bootstrapping %s", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			bootstrapOpts.OnStep = sub
			var err error
			targetCfg, err = setup.BootstrapWithClient(ctx, sshClient, capturedKey, bootstrapOpts, os.Stdout)
			if err != nil {
				return fmt.Errorf("bootstrap %s: %w", c.Target, err)
			}
			return nil
		}},

		// Step 3: Restore via temporary WireGuard tunnel.
		{Title: fmt.Sprintf("Restoring snapshot on %s", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			sub(fmt.Sprintf("Connecting to %s", c.Target))
			tunCfg, err := targetCfg.ToTunnelConfig()
			if err != nil {
				return fmt.Errorf("build tunnel config: %w", err)
			}
			tun := tunnel.NewClientTunnel(tunCfg)

			tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer tunCancel()
			tunErr := make(chan error, 1)
			go func() { tunErr <- tun.Start(tunCtx) }()

			select {
			case <-tun.Ready():
			case err := <-tunErr:
				return fmt.Errorf("tunnel failed to start: %w", err)
			case <-tunCtx.Done():
				return fmt.Errorf("tunnel start timed out")
			}

			agentClient := &http.Client{
				Timeout:   agentClientTimeout,
				Transport: &http.Transport{DialContext: tun.DialContext},
			}
			agentURL := fmt.Sprintf("http://%s:%d/health", targetCfg.AgentIP, tunnel.AgentAPIPort)
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("target agent unreachable after bootstrap: %w", err)
			}
			sub(fmt.Sprintf("Connected to %s", c.Target))

			sub(fmt.Sprintf("Restoring snapshot %s", snapID))
			if _, err := rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", map[string]string{"id": snapID}); err != nil {
				fmt.Fprintf(os.Stderr, "\nRestore failed. To retry manually:\n")
				fmt.Fprintf(os.Stderr, "  hop snap restore %s --host %s\n", snapID, c.Target)
				return fmt.Errorf("restore on %s: %w", c.Target, err)
			}
			return nil
		}},

		// Step 4: Switch default host.
		{Title: fmt.Sprintf("Setting default host to %q", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			if err := hostconfig.SetDefaultHost(c.Target); err != nil {
				return fmt.Errorf("set default host: %w", err)
			}
			return nil
		}},
	}

	if err := tui.RunSteps(ctx, steps); err != nil {
		return err
	}

	fmt.Println("\n" + ui.StepOK(fmt.Sprintf("Migration complete. Default host set to %q", c.Target)))
	fmt.Printf("Run 'hop up' to connect to %s.\n", c.Target)
	return nil
}
