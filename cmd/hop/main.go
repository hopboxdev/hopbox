package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/alecthomas/kong"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
)

// CLI is the top-level Kong struct.
type CLI struct {
	Verbose bool   `short:"v" help:"Verbose output."`
	Host    string `short:"H" help:"Host name from ~/.config/hopbox/hosts/."`

	Setup     SetupCmd    `cmd:"" help:"Bootstrap a new remote host."`
	Up        UpCmd       `cmd:"" help:"Bring up WireGuard tunnel and bridges."`
	Down      DownCmd     `cmd:"" help:"Tear down tunnel."`
	Daemon    DaemonCmd   `cmd:"" help:"Manage tunnel daemon."`
	Status    StatusCmd   `cmd:"" help:"Show tunnel and workspace health."`
	Services  ServicesCmd `cmd:"" help:"Manage workspace services."`
	Logs      LogsCmd     `cmd:"" help:"Stream service logs."`
	Code      CodeCmd     `cmd:"" help:"Open VS Code connected to the workspace."`
	RunScript RunCmd      `cmd:"" name:"run" help:"Execute a script from hopbox.yaml."`
	Snap      SnapCmd     `cmd:"" help:"Snapshot workspace state (create/restore/ls)."`
	To        ToCmd       `cmd:"" help:"Migrate workspace to a new host."`
	Bridge    BridgeCmd   `cmd:"" help:"Manage local-remote bridges."`
	Hosts     HostCmd     `cmd:"" name:"host" help:"Manage host registry."`
	Init      InitCmd     `cmd:"" help:"Generate hopbox.yaml scaffold."`
	Rotate    RotateCmd   `cmd:"" help:"Rotate WireGuard keys for a host."`
	Upgrade   UpgradeCmd  `cmd:"" help:"Upgrade hop binaries (client, helper, agent)."`
	Version   VersionCmd  `cmd:"" help:"Print version."`
}

func main() {
	var cli CLI
	k, err := kong.New(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox CLI — instant dev environments on your VPS"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			NoExpandSubcommands: true,
			Compact:             true,
		}),
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
// 3. default_host in ~/.config/hopbox/config.yaml
func resolveHost(globals *CLI) (string, error) {
	if globals.Host != "" {
		return globals.Host, nil
	}
	ws, err := manifest.Parse("hopbox.yaml")
	if err == nil && ws.Host != "" {
		return ws.Host, nil
	}
	cfg, err := hostconfig.LoadGlobalConfig()
	if err == nil && cfg.DefaultHost != "" {
		return cfg.DefaultHost, nil
	}
	return "", fmt.Errorf("--host <name> required (or set host: in hopbox.yaml, or run 'hop host default <name>')")
}

// probeAgent polls GET url until it returns 200 or timeout expires.
// client must dial through the WireGuard tunnel (tun.DialContext transport).
func probeAgent(ctx context.Context, url string, timeout time.Duration, client *http.Client) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
