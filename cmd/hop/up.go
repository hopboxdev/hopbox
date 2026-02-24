package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/daemon"
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
	Workspace  string `arg:"" optional:"" help:"Path to hopbox.yaml (default: ./hopbox.yaml)."`
	Foreground bool   `short:"f" help:"Run in foreground (don't daemonize)."`
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

	// Check for running daemon.
	daemonClient, err := daemon.NewClient(hostName)
	if err != nil {
		return err
	}
	daemonRunning := daemonClient.IsRunning()

	if c.Foreground {
		if daemonRunning {
			return fmt.Errorf("daemon already running for %q; use 'hop down' first, or run without --foreground", hostName)
		}
		return c.runForeground(globals, hostName, cfg)
	}

	// Daemon mode (default).
	if !daemonRunning {
		// Also check for stale state file from a dead process.
		if existing, _ := tunnel.LoadState(hostName); existing != nil {
			return fmt.Errorf("tunnel to %q is already running (PID %d); use 'hop down' to stop it first", hostName, existing.PID)
		}

		if err := c.launchDaemon(hostName); err != nil {
			return err
		}
		fmt.Println(ui.StepInfo("Starting tunnel daemon..."))
		if err := daemonClient.WaitForReady(15 * time.Second); err != nil {
			return fmt.Errorf("daemon failed to start: %w", err)
		}
	} else {
		fmt.Println(ui.StepOK("Tunnel daemon already running"))
	}

	// Get daemon status for display.
	status, err := daemonClient.Status()
	if err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}
	fmt.Println(ui.StepOK(fmt.Sprintf("Tunnel to %s up (%s)", cfg.Name, status.Interface)))

	// Run TUI phases (agent probe, manifest sync, packages).
	return c.runTUIPhases(hostName, cfg)
}

// runForeground runs the tunnel in the current process (original behavior).
func (c *UpCmd) runForeground(globals *CLI, hostName string, cfg *hostconfig.HostConfig) error {
	if existing, _ := tunnel.LoadState(hostName); existing != nil {
		return fmt.Errorf("tunnel to %q is already running (PID %d); press Ctrl-C in that session to stop it first", hostName, existing.PID)
	}

	tunCfg, err := cfg.ToTunnelConfig()
	if err != nil {
		return fmt.Errorf("convert tunnel config: %w", err)
	}

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

	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
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

	// TUI phases.
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	phases := c.buildTUIPhases(hostName, agentURL, agentClient, ws, wsPath)

	if len(phases) > 0 {
		if err := tui.RunPhases(ctx, "hop up", phases); err != nil {
			return err
		}
	}

	// Start bridges.
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

// runTUIPhases runs the agent probe and workspace sync phases.
// Used by daemon mode after the daemon is already running.
// Steps run sequentially with simple printed output (no animated TUI)
// because the daemon is already up and steps complete quickly.
func (c *UpCmd) runTUIPhases(hostName string, cfg *hostconfig.HostConfig) error {
	hostname := cfg.Name + ".hop"
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	ctx := context.Background()

	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	var ws *manifest.Workspace
	if _, err := os.Stat(wsPath); err == nil {
		var err error
		ws, err = manifest.Parse(wsPath)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	phases := c.buildTUIPhases(hostName, agentURL, agentClient, ws, wsPath)

	for _, phase := range phases {
		for _, step := range phase.Steps {
			msg := step.Title
			send := func(evt tui.StepEvent) { msg = evt.Message }
			err := step.Run(ctx, send)
			if err != nil {
				if step.NonFatal {
					fmt.Println(ui.Warn(msg + ": " + err.Error()))
					continue
				}
				fmt.Println(ui.StepFail(msg))
				return err
			}
			fmt.Println(ui.StepOK(msg))
		}
	}

	fmt.Println(ui.StepOK("Tunnel ready"))
	return nil
}

// buildTUIPhases constructs the TUI phases for agent probe and workspace sync.
func (c *UpCmd) buildTUIPhases(hostName, agentURL string, agentClient *http.Client, ws *manifest.Workspace, wsPath string) []tui.Phase {
	var phases []tui.Phase

	// Agent phase.
	agentSteps := []tui.Step{
		{Title: fmt.Sprintf("Probing agent at %s", agentURL), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("agent probe failed: %w", err)
			}
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
			send(tui.StepEvent{Message: "Agent is up"})
			return nil
		}},
	}
	phases = append(phases, tui.Phase{Title: "Agent", Steps: agentSteps})

	// Workspace phase (optional).
	if ws != nil {
		var wsSteps []tui.Step
		wsSteps = append(wsSteps, tui.Step{
			Title: fmt.Sprintf("Syncing manifest: %s", ws.Name),
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				rawManifest, err := os.ReadFile(wsPath)
				if err != nil {
					return err
				}
				if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); err != nil {
					return fmt.Errorf("manifest sync: %w", err)
				}
				send(tui.StepEvent{Message: "Manifest synced"})
				return nil
			},
			NonFatal: true,
		})
		if len(ws.Packages) > 0 {
			wsSteps = append(wsSteps, tui.Step{
				Title: fmt.Sprintf("Installing %d package(s)", len(ws.Packages)),
				Run: func(ctx context.Context, send func(tui.StepEvent)) error {
					pkgs := make([]map[string]string, 0, len(ws.Packages))
					for _, p := range ws.Packages {
						pkgs = append(pkgs, map[string]string{
							"name":    p.Name,
							"backend": p.Backend,
							"version": p.Version,
						})
					}
					if _, err := rpcclient.Call(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
						return fmt.Errorf("package install: %w", err)
					}
					send(tui.StepEvent{Message: "Packages installed"})
					return nil
				},
				NonFatal: true,
			})
		}
		phases = append(phases, tui.Phase{Title: "Workspace", Steps: wsSteps})
	}

	return phases
}

// launchDaemon starts the daemon process as a detached child.
func (c *UpCmd) launchDaemon(hostName string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	args := []string{"daemon", "start", hostName}
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	if _, err := os.Stat(wsPath); err == nil {
		abs, err := filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("resolve workspace path: %w", err)
		}
		args = append(args, "--workspace", abs)
	}

	logPath, err := daemon.LogPath(hostName)
	if err != nil {
		return err
	}
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch daemon: %w", err)
	}

	// Detach — don't wait for the child.
	go func() { _ = cmd.Wait() }()
	return nil
}
