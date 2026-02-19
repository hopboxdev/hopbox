package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/hopboxdev/hopbox/internal/bridge"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/version"
)

// SetupCmd bootstraps a new remote host.
type SetupCmd struct {
	Name   string `arg:"" help:"Name for this host."`
	Host   string `short:"H" required:"" help:"Remote host IP or hostname."`
	User   string `short:"u" default:"root" help:"SSH username."`
	SSHKey string `short:"k" help:"Path to SSH private key."`
	Port   int    `short:"p" default:"22" help:"SSH port."`
	Agent  string `help:"Path to hop-agent binary to upload."`
}

func (c *SetupCmd) Run() error {
	opts := setup.Options{
		Name:            c.Name,
		SSHHost:         c.Host,
		SSHPort:         c.Port,
		SSHUser:         c.User,
		SSHKeyPath:      c.SSHKey,
		AgentBinaryPath: c.Agent,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	_, err := setup.Bootstrap(ctx, opts, os.Stdout)
	return err
}

// UpCmd brings up the WireGuard tunnel and bridges.
type UpCmd struct {
	Workspace string `arg:"" optional:"" help:"Path to hopbox.yaml (default: ./hopbox.yaml)."`
	Host      string `short:"H" help:"Host name (overrides --host flag)."`
	SSH       bool   `help:"Fall back to SSH tunneling instead of WireGuard."`
}

func (c *UpCmd) Run(globals *CLI) error {
	hostName := globals.Host
	if c.Host != "" {
		hostName = c.Host
	}
	if hostName == "" {
		return fmt.Errorf("--host <name> required (or set in hopbox.yaml)")
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", hostName, err)
	}

	tunCfg := cfg.ToTunnelConfig()
	tun := tunnel.NewClientTunnel(tunCfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("Bringing up tunnel to %s (%s)...\n", cfg.Name, cfg.Endpoint)

	tunnelReady := make(chan struct{})
	tunnelErr := make(chan error, 1)

	go func() {
		close(tunnelReady)
		tunnelErr <- tun.Start(ctx)
	}()

	<-tunnelReady

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
				br := bridge.NewClipboardBridge(cfg.AgentIP)
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "clipboard bridge error: %v\n", err)
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge(cfg.AgentIP)
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "CDP bridge error: %v\n", err)
					}
				}(br)
			}
		}
	}

	// Probe /health
	agentURL := fmt.Sprintf("http://%s:%d/health", cfg.AgentIP, tunnel.AgentAPIPort)
	fmt.Printf("Probing agent at %s...\n", agentURL)

	probeCtx, probeCancel := context.WithTimeout(ctx, 0)
	defer probeCancel()
	_ = probeCtx

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

// DownCmd tears down the tunnel (no-op in foreground mode).
type DownCmd struct{}

func (c *DownCmd) Run() error {
	fmt.Println("In foreground mode, use Ctrl-C to stop the tunnel.")
	return nil
}

// StatusCmd shows tunnel and workspace health.
type StatusCmd struct {
	Host string `short:"H" help:"Host name."`
}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName := globals.Host
	if c.Host != "" {
		hostName = c.Host
	}
	if hostName == "" {
		return fmt.Errorf("--host <name> required")
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	agentURL := fmt.Sprintf("http://%s:%d/health", cfg.AgentIP, tunnel.AgentAPIPort)
	resp, err := http.Get(agentURL)
	if err != nil {
		fmt.Printf("Agent unreachable: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var health map[string]any
	if err := json.Unmarshal(body, &health); err != nil {
		fmt.Println(string(body))
		return nil
	}
	fmt.Printf("Host: %s\n", cfg.Name)
	fmt.Printf("Endpoint: %s\n", cfg.Endpoint)
	for k, v := range health {
		fmt.Printf("  %s: %v\n", k, v)
	}
	return nil
}

// ServicesCmd manages workspace services.
type ServicesCmd struct {
	Ls      ServicesLsCmd      `cmd:"" name:"ls" help:"List services."`
	Restart ServicesRestartCmd `cmd:"" name:"restart" help:"Restart a service."`
	Stop    ServicesStopCmd    `cmd:"" name:"stop" help:"Stop a service."`
}

// ServicesLsCmd lists services.
type ServicesLsCmd struct{}

func (c *ServicesLsCmd) Run(globals *CLI) error {
	return rpcCall(globals.Host, "services.list", nil)
}

// ServicesRestartCmd restarts a named service.
type ServicesRestartCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesRestartCmd) Run(globals *CLI) error {
	return rpcCall(globals.Host, "services.restart", map[string]string{"name": c.Name})
}

// ServicesStopCmd stops a named service.
type ServicesStopCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesStopCmd) Run(globals *CLI) error {
	return rpcCall(globals.Host, "services.stop", map[string]string{"name": c.Name})
}

// LogsCmd streams service logs.
type LogsCmd struct {
	Service string `arg:"" optional:"" help:"Service name (default: all)."`
}

func (c *LogsCmd) Run(globals *CLI) error {
	fmt.Println("Log streaming not yet implemented.")
	return nil
}

// ShellCmd drops into a remote shell.
type ShellCmd struct{}

func (c *ShellCmd) Run(globals *CLI) error {
	cfg, err := hostconfig.Load(globals.Host)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}
	fmt.Printf("ssh %s@%s\n", cfg.SSHUser, cfg.AgentIP)
	fmt.Println("Shell command (SSH via WireGuard IP) not yet implemented.")
	return nil
}

// RunCmd executes a named script from the manifest.
type RunCmd struct {
	Script string `arg:"" help:"Script name from hopbox.yaml."`
}

func (c *RunCmd) Run() error {
	return rpcCall("", "run.script", map[string]string{"name": c.Script})
}

// HostCmd manages the host registry.
type HostCmd struct {
	Add HostAddCmd `cmd:"" name:"add" help:"Add a host."`
	Rm  HostRmCmd  `cmd:"" name:"rm" help:"Remove a host."`
	Ls  HostLsCmd  `cmd:"" name:"ls" help:"List hosts."`
}

// HostAddCmd is a placeholder (use hop setup instead).
type HostAddCmd struct {
	Name string `arg:""`
}

func (c *HostAddCmd) Run() error {
	fmt.Println("Use 'hop setup' to add a host via SSH bootstrap.")
	return nil
}

// HostRmCmd removes a host config.
type HostRmCmd struct {
	Name string `arg:""`
}

func (c *HostRmCmd) Run() error {
	return hostconfig.Delete(c.Name)
}

// HostLsCmd lists registered hosts.
type HostLsCmd struct{}

func (c *HostLsCmd) Run() error {
	names, err := hostconfig.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("No hosts configured. Use 'hop setup' to add one.")
		return nil
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct{}

func (c *InitCmd) Run() error {
	scaffold := `name: myapp

services:
  app:
    type: docker
    image: myapp:latest
    ports: [8080]

bridges:
  - type: clipboard

session:
  manager: zellij
  name: myapp
`
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}
	return os.WriteFile(path, []byte(scaffold), 0644)
}

// VersionCmd prints version info.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("hop %s (commit %s, built %s)\n",
		version.Version, version.Commit, version.Date)
	return nil
}

// CLI is the top-level Kong struct.
type CLI struct {
	Verbose bool   `short:"v" help:"Verbose output."`
	Host    string `short:"H" help:"Host name from ~/.config/hopbox/hosts/."`

	Setup     SetupCmd    `cmd:"" help:"Bootstrap a new remote host."`
	Up        UpCmd       `cmd:"" help:"Bring up WireGuard tunnel and bridges."`
	Down      DownCmd     `cmd:"" help:"Tear down tunnel (use Ctrl-C in foreground mode)."`
	Status    StatusCmd   `cmd:"" help:"Show tunnel and workspace health."`
	Services  ServicesCmd `cmd:"" help:"Manage workspace services."`
	Logs      LogsCmd     `cmd:"" help:"Stream service logs."`
	Shell     ShellCmd    `cmd:"" help:"Drop into remote shell."`
	RunScript RunCmd      `cmd:"" name:"run" help:"Execute a script from hopbox.yaml."`
	Hosts     HostCmd     `cmd:"" name:"host" help:"Manage host registry."`
	Init      InitCmd     `cmd:"" help:"Generate hopbox.yaml scaffold."`
	Version   VersionCmd  `cmd:"" help:"Print version."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox CLI — instant dev environments on your VPS"),
		kong.UsageOnError(),
	)
	err := ctx.Run(&cli)
	ctx.FatalIfErrorf(err)
}

// rpcCall makes an RPC request to the agent and prints the result.
func rpcCall(hostName, method string, params any) error {
	if hostName == "" {
		return fmt.Errorf("--host <name> required")
	}
	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"method": method,
		"params": params,
	})

	url := fmt.Sprintf("http://%s:%d/rpc", cfg.AgentIP, tunnel.AgentAPIPort)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("RPC call: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	return nil
}
