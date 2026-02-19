package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

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

	_, err := setup.Bootstrap(ctx, opts, os.Stdout)
	return err
}

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
				br := bridge.NewClipboardBridge(cfg.AgentIP)
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintf(os.Stderr, "clipboard bridge error: %v\n", err)
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge(cfg.AgentIP)
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintf(os.Stderr, "CDP bridge error: %v\n", err)
					}
				}(br)
			}
		}
	}

	// Probe /health with retry loop.
	agentURL := fmt.Sprintf("http://%s:%d/health", cfg.AgentIP, tunnel.AgentAPIPort)
	fmt.Printf("Probing agent at %s...\n", agentURL)

	if err := probeAgent(ctx, agentURL, 10*time.Second); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: agent probe failed: %v\n", err)
	} else {
		fmt.Println("Agent is up.")
	}

	// Sync manifest to agent so scripts, backup, and services reload.
	if ws != nil {
		rawManifest, readErr := os.ReadFile(wsPath)
		if readErr == nil {
			if _, syncErr := rpcCallResult(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); syncErr != nil {
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
		if _, err := rpcCallResult(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: package installation failed: %v\n", err)
		} else {
			fmt.Println("Packages installed.")
		}
	}

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
type StatusCmd struct{}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "HOST\t%s\n", cfg.Name)
	_, _ = fmt.Fprintf(tw, "ENDPOINT\t%s\n", cfg.Endpoint)
	_, _ = fmt.Fprintf(tw, "AGENT-IP\t%s\n", cfg.AgentIP)

	agentURL := fmt.Sprintf("http://%s:%d/health", cfg.AgentIP, tunnel.AgentAPIPort)
	resp, err := http.Get(agentURL)
	if err != nil {
		_, _ = fmt.Fprintf(tw, "AGENT\tunreachable: %v\n", err)
		_ = tw.Flush()
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	var health map[string]any
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &health)

	tunnelStatus := "down"
	if v, ok := health["tunnel"]; ok {
		if b, ok := v.(bool); ok && b {
			tunnelStatus = "up"
		}
	}
	agentStatus := "ok"
	if v, ok := health["status"]; ok {
		agentStatus = fmt.Sprint(v)
	}
	_, _ = fmt.Fprintf(tw, "TUNNEL\t%s\n", tunnelStatus)
	_, _ = fmt.Fprintf(tw, "AGENT\t%s\n", agentStatus)
	_ = tw.Flush()

	// Fetch and display services.
	svcResult, err := rpcCallResult(hostName, "services.list", nil)
	if err == nil {
		var svcs []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Running bool   `json:"running"`
			Error   string `json:"error,omitempty"`
		}
		if json.Unmarshal(svcResult, &svcs) == nil && len(svcs) > 0 {
			fmt.Println("\nSERVICES")
			sw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(sw, "  NAME\tTYPE\tSTATUS\n")
			for _, s := range svcs {
				status := "stopped"
				if s.Running {
					status = "running"
				}
				if s.Error != "" {
					status = "error: " + s.Error
				}
				_, _ = fmt.Fprintf(sw, "  %s\t%s\t%s\n", s.Name, s.Type, status)
			}
			_ = sw.Flush()
		}
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
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "services.list", nil)
}

// ServicesRestartCmd restarts a named service.
type ServicesRestartCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesRestartCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "services.restart", map[string]string{"name": c.Name})
}

// ServicesStopCmd stops a named service.
type ServicesStopCmd struct {
	Name string `arg:"" help:"Service name."`
}

func (c *ServicesStopCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "services.stop", map[string]string{"name": c.Name})
}

// LogsCmd streams service logs.
type LogsCmd struct {
	Service string `arg:"" optional:"" help:"Service name (default: all)."`
}

func (c *LogsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "logs.stream", map[string]string{"name": c.Service})
}

// ShellCmd drops into a remote shell.
type ShellCmd struct{}

func (c *ShellCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	user := cfg.SSHUser
	if user == "" {
		user = "root"
	}

	sshArgs := []string{"-t", user + "@" + cfg.AgentIP}

	// Attach to session manager if a local hopbox.yaml specifies one.
	if ws, wsErr := manifest.Parse("hopbox.yaml"); wsErr == nil && ws.Session != nil {
		name := ws.Session.Name
		if name == "" {
			name = ws.Name
		}
		switch ws.Session.Manager {
		case "zellij":
			sshArgs = append(sshArgs, "zellij", "attach", "--create", name)
		case "tmux":
			sshArgs = append(sshArgs, "tmux", "new-session", "-A", "-s", name)
		}
	}

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunCmd executes a named script from the manifest.
type RunCmd struct {
	Script string `arg:"" help:"Script name from hopbox.yaml."`
}

func (c *RunCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "run.script", map[string]string{"name": c.Script})
}

// SnapCmd manages workspace snapshots.
type SnapCmd struct {
	Create  SnapCreateCmd  `cmd:"" name:"create" help:"Create a new snapshot." default:"1"`
	Restore SnapRestoreCmd `cmd:"" name:"restore" help:"Restore from a snapshot."`
	Ls      SnapLsCmd      `cmd:"" name:"ls" help:"List snapshots."`
}

// SnapCreateCmd creates a new snapshot.
type SnapCreateCmd struct{}

func (c *SnapCreateCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	result, err := rpcCallResult(hostName, "snap.create", nil)
	if err != nil {
		return err
	}
	var snap struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal(result, &snap); err == nil && snap.SnapshotID != "" {
		fmt.Printf("Snapshot created: %s\n", snap.SnapshotID)
		return nil
	}
	fmt.Println(string(result))
	return nil
}

// SnapRestoreCmd restores a workspace from a snapshot.
type SnapRestoreCmd struct {
	ID          string `arg:"" help:"Snapshot ID to restore."`
	RestorePath string `help:"Restore root path (default: /)."`
}

func (c *SnapRestoreCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	params := map[string]string{"id": c.ID}
	if c.RestorePath != "" {
		params["restore_path"] = c.RestorePath
	}
	return rpcCall(hostName, "snap.restore", params)
}

// SnapLsCmd lists snapshots.
type SnapLsCmd struct{}

func (c *SnapLsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	return rpcCall(hostName, "snap.list", nil)
}

// ToCmd migrates the workspace to a new host.
type ToCmd struct {
	Target string `arg:"" help:"Target host name (must be set up with 'hop setup')."`
}

func (c *ToCmd) Run(globals *CLI) error {
	sourceHost, err := resolveHost(globals)
	if err != nil {
		return fmt.Errorf("source host: %w", err)
	}
	if c.Target == sourceHost {
		return fmt.Errorf("target host must differ from source host")
	}
	if _, err := hostconfig.Load(c.Target); err != nil {
		return fmt.Errorf("target host %q not found: run 'hop setup %s --host <ip>' first", c.Target, c.Target)
	}

	fmt.Printf("Step 1/2: Creating snapshot on %s...\n", sourceHost)
	snapResult, err := rpcCallResult(sourceHost, "snap.create", nil)
	if err != nil {
		return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
	}
	var snap struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal(snapResult, &snap); err != nil || snap.SnapshotID == "" {
		return fmt.Errorf("could not determine snapshot ID from response: %s", string(snapResult))
	}
	fmt.Printf("Snapshot created: %s\n", snap.SnapshotID)

	fmt.Printf("Step 2/2: Restoring snapshot %s on %s...\n", snap.SnapshotID, c.Target)
	if err := rpcCall(c.Target, "snap.restore", map[string]string{"id": snap.SnapshotID}); err != nil {
		return fmt.Errorf("restore on %s: %w", c.Target, err)
	}

	fmt.Printf("\nMigration complete.\n")
	fmt.Printf("Run 'hop up --host %s' to connect to the new host.\n", c.Target)
	return nil
}

// BridgeCmd manages local-remote bridges.
type BridgeCmd struct {
	Ls      BridgeLsCmd      `cmd:"" name:"ls" help:"List configured bridges."`
	Restart BridgeRestartCmd `cmd:"" name:"restart" help:"Restart a bridge."`
}

// BridgeLsCmd lists bridges from the local manifest.
type BridgeLsCmd struct {
	Workspace string `short:"w" help:"Path to hopbox.yaml." default:"hopbox.yaml"`
}

func (c *BridgeLsCmd) Run() error {
	ws, err := manifest.Parse(c.Workspace)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if len(ws.Bridges) == 0 {
		fmt.Println("No bridges configured.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "TYPE\tSTATUS\n")
	for _, b := range ws.Bridges {
		_, _ = fmt.Fprintf(tw, "%s\tconfigured\n", b.Type)
	}
	return tw.Flush()
}

// BridgeRestartCmd restarts a bridge (requires tunnel to be up via hop up).
type BridgeRestartCmd struct {
	Type string `arg:"" help:"Bridge type (clipboard, cdp)."`
}

func (c *BridgeRestartCmd) Run() error {
	return fmt.Errorf("bridge restart requires restarting 'hop up': run 'hop down' then 'hop up'")
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
host: ""

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
	Snap      SnapCmd     `cmd:"" help:"Snapshot workspace state (create/restore/ls)."`
	To        ToCmd       `cmd:"" help:"Migrate workspace to a new host."`
	Bridge    BridgeCmd   `cmd:"" help:"Manage local-remote bridges."`
	Hosts     HostCmd     `cmd:"" name:"host" help:"Manage host registry."`
	Init      InitCmd     `cmd:"" help:"Generate hopbox.yaml scaffold."`
	Version   VersionCmd  `cmd:"" help:"Print version."`
}

func main() {
	var cli CLI
	k, err := kong.New(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox CLI — instant dev environments on your VPS"),
		kong.UsageOnError(),
	)
	if err != nil {
		panic(err)
	}

	args := os.Args[1:]
	// No args or bare "help" → print usage and exit 0 (not an error).
	// Passing --help to k.Parse lets Kong handle the print+exit itself.
	if len(args) == 0 || (len(args) == 1 && args[0] == "help") {
		_, _ = k.Parse([]string{"--help"})
		os.Exit(0) // unreachable; defensive fallback
	}

	ctx, err := k.Parse(args)
	k.FatalIfErrorf(err)
	k.FatalIfErrorf(ctx.Run(&cli))
}

// resolveHost returns the host name to use, in order of precedence:
// 1. --host flag (globals.Host)
// 2. host: field in ./hopbox.yaml
// 3. the sole configured host (if exactly one exists)
func resolveHost(globals *CLI) (string, error) {
	if globals.Host != "" {
		return globals.Host, nil
	}
	ws, err := manifest.Parse("hopbox.yaml")
	if err == nil && ws.Host != "" {
		return ws.Host, nil
	}
	// Auto-select when there is exactly one host configured.
	names, err := hostconfig.List()
	if err == nil && len(names) == 1 {
		return names[0], nil
	}
	return "", fmt.Errorf("--host <name> required (or set host: in hopbox.yaml)")
}

// probeAgent polls GET url until it returns 200 or timeout expires.
func probeAgent(ctx context.Context, url string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		lastErr = err
		select {
		case <-deadline.Done():
			return fmt.Errorf("agent not reachable within %s: %w", timeout, lastErr)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// rpcCallResult makes an RPC request to the agent and returns the result JSON.
func rpcCallResult(hostName, method string, params any) (json.RawMessage, error) {
	if hostName == "" {
		return nil, fmt.Errorf("--host <name> required")
	}
	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return nil, fmt.Errorf("load host config: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"method": method,
		"params": params,
	})

	url := fmt.Sprintf("http://%s:%d/rpc", cfg.AgentIP, tunnel.AgentAPIPort)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("RPC call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse RPC response: %w", err)
	}
	if rpcResp.Error != "" {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error)
	}
	return rpcResp.Result, nil
}

// rpcCall makes an RPC request to the agent and prints the result.
func rpcCall(hostName, method string, params any) error {
	result, err := rpcCallResult(hostName, method, params)
	if err != nil {
		return err
	}
	fmt.Println(string(result))
	return nil
}
