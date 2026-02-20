package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/tunnel"
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
	tun := tunnel.NewClientTunnel(tunCfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("Bringing up tunnel to %s (%s)...\n", cfg.Name, cfg.Endpoint)

	tunnelErr := make(chan error, 1)

	go func() {
		tunnelErr <- tun.Start(ctx)
	}()

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

	// Build an HTTP client that routes through the WireGuard netstack.
	// The standard net/http dialer uses the OS network stack, which has no
	// route to 10.10.0.2 — only tun.DialContext can reach it.
	agentClient := &http.Client{
		Timeout: agentClientTimeout,
		Transport: &http.Transport{
			DialContext: tun.DialContext,
		},
	}

	// Probe /health with retry loop.
	agentURL := fmt.Sprintf("http://%s:%d/health", cfg.AgentIP, tunnel.AgentAPIPort)
	fmt.Printf("Probing agent at %s...\n", agentURL)

	if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: agent probe failed: %v\n", err)
	} else {
		fmt.Println("Agent is up.")
	}

	// Sync manifest to agent so scripts, backup, and services reload.
	if ws != nil {
		rawManifest, readErr := os.ReadFile(wsPath)
		if readErr == nil {
			if _, syncErr := rpcclient.CallVia(agentClient, hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); syncErr != nil {
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
		if _, err := rpcclient.CallVia(agentClient, hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: package installation failed: %v\n", err)
		} else {
			fmt.Println("Packages installed.")
		}
	}

	// Start TCP proxies so other hop commands can reach the agent via the OS network.
	// On macOS, 10.10.0.2 only exists inside this process (netstack); proxies expose
	// the tunnel on localhost so external processes can use it.
	var proxies []*tunnel.Proxy
	defer func() {
		for _, p := range proxies {
			p.Stop()
		}
	}()

	startProxy := func(label, localAddr, remoteAddr string) *tunnel.Proxy {
		p, proxyErr := tunnel.StartProxy(ctx, tunnel.ProxyConfig{
			LocalAddr:  localAddr,
			RemoteAddr: remoteAddr,
			Label:      label,
		}, tun.DialContext)
		if proxyErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: proxy %s: %v\n", label, proxyErr)
			return nil
		}
		proxies = append(proxies, p)
		return p
	}

	agentProxy := startProxy("agent-api",
		"127.0.0.1:4200",
		fmt.Sprintf("%s:%d", cfg.AgentIP, tunnel.AgentAPIPort))
	if agentProxy != nil {
		fmt.Printf("Forwarding %s → agent API\n", agentProxy.LocalAddr())
	}

	sshProxy := startProxy("ssh", "127.0.0.1:2222", cfg.AgentIP+":22")
	if sshProxy != nil {
		fmt.Printf("Forwarding %s → SSH\n", sshProxy.LocalAddr())
	}

	// Start proxies for declared service ports.
	// Port specs are "hostPort" or "hostPort:containerPort"; the proxy always
	// binds 127.0.0.1:<hostPort> locally and forwards to agent:<hostPort>.
	servicePorts := make(map[string]string)
	if ws != nil {
		for svcName, svc := range ws.Services {
			for _, portSpec := range svc.Ports {
				hostPort := portSpec
				if i := strings.IndexByte(portSpec, ':'); i >= 0 {
					hostPort = portSpec[:i]
				}
				label := fmt.Sprintf("%s:%s", svcName, hostPort)
				p := startProxy(label,
					fmt.Sprintf("127.0.0.1:%s", hostPort),
					fmt.Sprintf("%s:%s", cfg.AgentIP, hostPort))
				if p != nil {
					addr := p.LocalAddr().String()
					fmt.Printf("Forwarding %s → %s\n", addr, label)
					servicePorts[label] = addr
				}
			}
		}
	}

	// Write tunnel state so other hop commands (hop status, hop shell, etc.) can
	// find the proxy addresses without needing kernel WireGuard routing.
	state := &tunnel.TunnelState{
		PID:          os.Getpid(),
		Host:         hostName,
		ServicePorts: servicePorts,
		StartedAt:    time.Now(),
	}
	if agentProxy != nil {
		state.AgentAPIAddr = agentProxy.LocalAddr().String()
	}
	if sshProxy != nil {
		state.SSHAddr = sshProxy.LocalAddr().String()
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
