package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

const (
	daemonAgentClientTimeout = 5 * time.Second
)

// Config holds everything the daemon needs to start.
type Config struct {
	HostName string
	TunCfg   tunnel.Config
	Manifest *manifest.Workspace // nil if no workspace
}

// Run starts the daemon and blocks until shutdown.
// This is the main entry point for `hop daemon start`.
func Run(cfg Config) error {
	// Ignore SIGHUP so the daemon survives terminal close.
	signal.Ignore(syscall.SIGHUP)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		return fmt.Errorf("hopbox helper is not running; install with 'sudo hop-helper --install'")
	}

	// Create TUN device via helper.
	tunFile, ifName, err := helperClient.CreateTUN(cfg.TunCfg.MTU)
	if err != nil {
		return fmt.Errorf("create TUN device: %w", err)
	}

	tun := tunnel.NewKernelTunnel(cfg.TunCfg, tunFile, ifName)

	tunnelErr := make(chan error, 1)
	go func() {
		tunnelErr <- tun.Start(ctx)
	}()

	// Wait for TUN ready.
	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Configure TUN (IP + route) via helper.
	localIP := strings.TrimSuffix(cfg.TunCfg.LocalIP, "/24")
	peerIP := strings.TrimSuffix(cfg.TunCfg.PeerIP, "/32")
	if err := helperClient.ConfigureTUN(tun.InterfaceName(), localIP, peerIP); err != nil {
		tun.Stop()
		return fmt.Errorf("configure TUN: %w", err)
	}
	defer func() { _ = helperClient.CleanupTUN(tun.InterfaceName()) }()

	hostname := cfg.HostName + ".hop"
	if err := helperClient.AddHost(peerIP, hostname); err != nil {
		tun.Stop()
		return fmt.Errorf("add host entry: %w", err)
	}
	defer func() { _ = helperClient.RemoveHost(hostname) }()

	log.Printf("interface %s up, %s → %s", tun.InterfaceName(), localIP, hostname)

	// Start bridges.
	var bridges []bridge.Bridge
	if cfg.Manifest != nil {
		for _, b := range cfg.Manifest.Bridges {
			switch b.Type {
			case "clipboard":
				br := bridge.NewClipboardBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						log.Printf("clipboard bridge error: %v", err)
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						log.Printf("CDP bridge error: %v", err)
					}
				}(br)
			}
		}
	}

	// Start port forwarder.
	pf := bridge.NewPortForwarder(cfg.HostName, tunnel.ServerIP,
		bridge.WithInterval(3*time.Second),
		bridge.WithOnForward(func(port int) {
			log.Printf("forwarding localhost:%d", port)
		}),
		bridge.WithOnUnforward(func(port int) {
			log.Printf("stopped forwarding localhost:%d", port)
		}),
	)
	go func() { _ = pf.Run(ctx) }()
	defer pf.Stop()

	// Write state file.
	state := &tunnel.TunnelState{
		PID:         os.Getpid(),
		Host:        cfg.HostName,
		Hostname:    hostname,
		Interface:   tun.InterfaceName(),
		StartedAt:   time.Now(),
		Connected:   true,
		LastHealthy: time.Now(),
	}
	if err := tunnel.WriteState(state); err != nil {
		log.Printf("write tunnel state: %v", err)
	}
	defer func() { _ = tunnel.RemoveState(cfg.HostName) }()

	// Start ConnMonitor.
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)
	agentClient := &http.Client{Timeout: daemonAgentClientTimeout}

	monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: agentURL,
		Client:    agentClient,
		OnStateChange: func(evt tunnel.ConnEvent) {
			switch evt.State {
			case tunnel.ConnStateDisconnected:
				log.Printf("agent unreachable — waiting for reconnection...")
				state.Connected = false
			case tunnel.ConnStateConnected:
				log.Printf("agent reconnected (was down for %s)", evt.Duration.Round(time.Second))
				state.Connected = true
				state.LastHealthy = evt.At
			}
			if err := tunnel.WriteState(state); err != nil {
				log.Printf("update tunnel state: %v", err)
			}
		},
		OnHealthy: func(t time.Time) {
			state.LastHealthy = t
			state.ForwardedPorts = pf.PortInfo()
			if err := tunnel.WriteState(state); err != nil {
				log.Printf("update tunnel state: %v", err)
			}
		},
	})
	go monitor.Run(ctx)

	// Build daemon handler for IPC.
	d := &daemonHandler{
		state:   state,
		bridges: bridges,
		cancel:  cancel,
	}

	// Start IPC server.
	sockPath, err := SocketPath(cfg.HostName)
	if err != nil {
		return fmt.Errorf("socket path: %w", err)
	}
	// Ensure the socket directory exists.
	if err := os.MkdirAll(strings.TrimSuffix(sockPath, "/"+cfg.HostName+".sock"), 0700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	srv := NewServer(sockPath, d)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start IPC server: %w", err)
	}
	defer srv.Stop()

	log.Printf("daemon ready (PID %d)", os.Getpid())

	// Block until shutdown.
	select {
	case <-ctx.Done():
		log.Println("shutting down...")
	case err := <-tunnelErr:
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	}

	return nil
}

// daemonHandler implements Handler for the IPC server.
type daemonHandler struct {
	state   *tunnel.TunnelState
	bridges []bridge.Bridge
	cancel  context.CancelFunc
}

func (d *daemonHandler) HandleStatus() *DaemonStatus {
	var bridgeNames []string
	for _, b := range d.bridges {
		bridgeNames = append(bridgeNames, b.Status())
	}
	return &DaemonStatus{
		PID:         d.state.PID,
		Connected:   d.state.Connected,
		LastHealthy: d.state.LastHealthy,
		Interface:   d.state.Interface,
		StartedAt:   d.state.StartedAt,
		Bridges:     bridgeNames,
	}
}

func (d *daemonHandler) HandleShutdown() {
	d.cancel()
}
