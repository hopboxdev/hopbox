package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/tui"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
	"github.com/hopboxdev/hopbox/internal/version"
)

const (
	agentClientTimeout = 5 * time.Second
	agentProbeTimeout  = 10 * time.Second
)

// UpCmd brings up the WireGuard tunnel and bridges.
type UpCmd struct {
	Workspace string `arg:"" optional:"" help:"Path to hopbox.yaml (default: ./hopbox.yaml)."`
	SSH       bool   `help:"Fall back to SSH tunneling instead of WireGuard."`
}

func (c *UpCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", hostName, err)
	}

	if existing, _ := tunnel.LoadState(hostName); existing != nil {
		return fmt.Errorf("tunnel to %q is already running (PID %d); press Ctrl-C in that session to stop it first", hostName, existing.PID)
	}

	tunCfg, err := cfg.ToTunnelConfig()
	if err != nil {
		return fmt.Errorf("convert tunnel config: %w", err)
	}

	// The helper daemon creates the utun device (requires root) and passes
	// the fd back via SCM_RIGHTS. This lets hop run unprivileged.
	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		return fmt.Errorf("hopbox helper is not running; install with 'sudo hop-helper --install' or re-run 'hop setup'")
	}

	tunFile, ifName, err := helperClient.CreateTUN(tunCfg.MTU)
	if err != nil {
		return fmt.Errorf("create TUN device: %w", err)
	}

	tun := tunnel.NewKernelTunnel(tunCfg, tunFile, ifName)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Println(ui.StepOK(fmt.Sprintf("Bringing up tunnel to %s (%s)", cfg.Name, cfg.Endpoint)))

	tunnelErr := make(chan error, 1)

	go func() {
		tunnelErr <- tun.Start(ctx)
	}()

	// Wait for the TUN device to be ready.
	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Configure the TUN interface (IP + route) via the privileged helper.
	localIP := strings.TrimSuffix(tunCfg.LocalIP, "/24")
	peerIP := strings.TrimSuffix(tunCfg.PeerIP, "/32")
	if err := helperClient.ConfigureTUN(tun.InterfaceName(), localIP, peerIP); err != nil {
		tun.Stop()
		return fmt.Errorf("configure TUN: %w", err)
	}
	defer func() { _ = helperClient.CleanupTUN(tun.InterfaceName()) }()

	hostname := cfg.Name + ".hop"
	if err := helperClient.AddHost(peerIP, hostname); err != nil {
		tun.Stop()
		return fmt.Errorf("add host entry: %w", err)
	}
	defer func() { _ = helperClient.RemoveHost(hostname) }()

	fmt.Println(ui.StepOK(fmt.Sprintf("Interface %s up, %s → %s", tun.InterfaceName(), localIP, hostname)))

	// Load workspace manifest.
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	var ws *manifest.Workspace
	if _, err := os.Stat(wsPath); err == nil {
		ws, err = manifest.Parse(wsPath)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	// Application setup steps via TUI runner.
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	var steps []tui.Step
	steps = append(steps, tui.Step{
		Title: fmt.Sprintf("Probing agent at %s", agentURL),
		Run: func(ctx context.Context, sub func(string)) error {
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("agent probe failed: %w", err)
			}
			// Check agent version.
			if resp, err := agentClient.Get(agentURL); err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				var health map[string]any
				if json.Unmarshal(body, &health) == nil {
					if agentVer, ok := health["version"].(string); ok && agentVer != version.Version {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf(
							"agent version %q differs from client %q — run 'hop upgrade' to sync",
							agentVer, version.Version)))
					}
				}
			}
			sub("Agent is up")
			return nil
		},
	})

	if ws != nil {
		steps = append(steps, tui.Step{
			Title: fmt.Sprintf("Loading workspace: %s", ws.Name),
			Run: func(ctx context.Context, sub func(string)) error {
				rawManifest, err := os.ReadFile(wsPath)
				if err != nil {
					return nil // non-fatal
				}
				if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); err != nil {
					_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("manifest sync failed: %v", err)))
				} else {
					sub("Manifest synced")
				}
				return nil
			},
		})
	}

	if ws != nil && len(ws.Packages) > 0 {
		steps = append(steps, tui.Step{
			Title: fmt.Sprintf("Installing %d package(s)", len(ws.Packages)),
			Run: func(ctx context.Context, sub func(string)) error {
				pkgs := make([]map[string]string, 0, len(ws.Packages))
				for _, p := range ws.Packages {
					pkgs = append(pkgs, map[string]string{
						"name":    p.Name,
						"backend": p.Backend,
						"version": p.Version,
					})
				}
				if _, err := rpcclient.Call(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
					_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("package installation failed: %v", err)))
				} else {
					sub("Packages installed")
				}
				return nil
			},
		})
	}

	if len(steps) > 0 {
		if err := tui.RunSteps(ctx, steps); err != nil {
			return err
		}
	}

	// Start bridges (after RunSteps, before monitoring).
	var bridges []bridge.Bridge
	if ws != nil {
		for _, b := range ws.Bridges {
			switch b.Type {
			case "clipboard":
				br := bridge.NewClipboardBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("clipboard bridge error: %v", err)))
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("CDP bridge error: %v", err)))
					}
				}(br)
			}
		}
	}

	// Write tunnel state.
	state := &tunnel.TunnelState{
		PID:         os.Getpid(),
		Host:        hostName,
		Hostname:    hostname,
		Interface:   tun.InterfaceName(),
		StartedAt:   time.Now(),
		Connected:   true,
		LastHealthy: time.Now(),
	}
	if err := tunnel.WriteState(state); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("write tunnel state: %v", err)))
	}
	defer func() { _ = tunnel.RemoveState(hostName) }()

	if globals.Verbose {
		for _, br := range bridges {
			fmt.Println(br.Status())
		}
	}

	fmt.Println(ui.StepOK("Tunnel up. Press Ctrl-C to stop"))

	monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: agentURL,
		Client:    agentClient,
		OnStateChange: func(evt tunnel.ConnEvent) {
			switch evt.State {
			case tunnel.ConnStateDisconnected:
				fmt.Printf("\n[%s] Agent unreachable — waiting for reconnection...\n",
					evt.At.Format("15:04:05"))
				state.Connected = false
			case tunnel.ConnStateConnected:
				fmt.Printf("[%s] Agent reconnected (was down for %s)\n",
					evt.At.Format("15:04:05"), evt.Duration.Round(time.Second))
				state.Connected = true
				state.LastHealthy = evt.At
			}
			if err := tunnel.WriteState(state); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("update tunnel state: %v", err)))
			}
		},
		OnHealthy: func(t time.Time) {
			state.LastHealthy = t
			if err := tunnel.WriteState(state); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("update tunnel state: %v", err)))
			}
		},
	})
	go monitor.Run(ctx)

	// Block until Ctrl-C
	select {
	case <-ctx.Done():
		fmt.Println("\n" + ui.StepRun("Shutting down..."))
	case err := <-tunnelErr:
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	}

	return nil
}
