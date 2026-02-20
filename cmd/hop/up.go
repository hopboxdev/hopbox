package main

import (
	"bufio"
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
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
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
	tun := tunnel.NewKernelTunnel(tunCfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("Bringing up tunnel to %s (%s)...\n", cfg.Name, cfg.Endpoint)

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

	// Configure the TUN interface via the privileged helper.
	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		tun.Stop()
		return fmt.Errorf("hopbox helper is not running; install with 'sudo hop-helper --install' or re-run 'hop setup'")
	}

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

	fmt.Printf("Interface %s up, %s → %s\n", tun.InterfaceName(), localIP, hostname)

	// Load workspace manifest if provided or if hopbox.yaml exists locally.
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
		fmt.Printf("Loaded workspace: %s\n", ws.Name)
	}

	// Start bridges
	var bridges []bridge.Bridge
	if ws != nil {
		for _, b := range ws.Bridges {
			switch b.Type {
			case "clipboard":
				br := bridge.NewClipboardBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintf(os.Stderr, "clipboard bridge error: %v\n", err)
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintf(os.Stderr, "CDP bridge error: %v\n", err)
					}
				}(br)
			}
		}
	}

	// With kernel TUN, the agent is reachable via the OS network stack.
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	// Probe /health with retry loop.
	fmt.Printf("Probing agent at %s...\n", agentURL)

	if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: agent probe failed: %v\n", err)
	} else {
		fmt.Println("Agent is up.")

		// Check agent version and offer to upgrade if it differs from the client.
		if resp, err := agentClient.Get(agentURL); err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			var health map[string]any
			if json.Unmarshal(body, &health) == nil {
				if agentVer, ok := health["version"].(string); ok && agentVer != version.Version {
					fmt.Printf("Agent version %q differs from client %q. Upgrade agent? [y/N] ", agentVer, version.Version)
					scanner := bufio.NewScanner(os.Stdin)
					if scanner.Scan() && strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
						if err := setup.UpgradeAgent(ctx, cfg, os.Stdout, version.Version); err != nil {
							_, _ = fmt.Fprintf(os.Stderr, "Warning: agent upgrade failed: %v\n", err)
						} else if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
							_, _ = fmt.Fprintf(os.Stderr, "Warning: post-upgrade agent probe failed: %v\n", err)
						} else {
							fmt.Println("Agent upgraded and reachable.")
						}
					}
				}
			}
		}
	}

	// Sync manifest to agent so scripts, backup, and services reload.
	if ws != nil {
		rawManifest, readErr := os.ReadFile(wsPath)
		if readErr == nil {
			if _, syncErr := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); syncErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: manifest sync failed: %v\n", syncErr)
			} else {
				fmt.Println("Manifest synced.")
			}
		}
	}

	// Install packages declared in the manifest.
	if ws != nil && len(ws.Packages) > 0 {
		fmt.Printf("Installing %d package(s)...\n", len(ws.Packages))
		pkgs := make([]map[string]string, 0, len(ws.Packages))
		for _, p := range ws.Packages {
			pkgs = append(pkgs, map[string]string{
				"name":    p.Name,
				"backend": p.Backend,
				"version": p.Version,
			})
		}
		if _, err := rpcclient.Call(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: package installation failed: %v\n", err)
		} else {
			fmt.Println("Packages installed.")
		}
	}

	// Write tunnel state so other hop commands can find the tunnel.
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
		_, _ = fmt.Fprintf(os.Stderr, "Warning: write tunnel state: %v\n", err)
	}
	defer func() { _ = tunnel.RemoveState(hostName) }()

	if globals.Verbose {
		for _, br := range bridges {
			fmt.Println(br.Status())
		}
	}

	fmt.Println("Tunnel up. Press Ctrl-C to stop.")

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
				_, _ = fmt.Fprintf(os.Stderr, "Warning: update tunnel state: %v\n", err)
			}
		},
		OnHealthy: func(t time.Time) {
			state.LastHealthy = t
			if err := tunnel.WriteState(state); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: update tunnel state: %v\n", err)
			}
		},
	})
	go monitor.Run(ctx)

	// Block until Ctrl-C
	select {
	case <-ctx.Done():
		fmt.Println("\nShutting down...")
	case err := <-tunnelErr:
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	}

	return nil
}
