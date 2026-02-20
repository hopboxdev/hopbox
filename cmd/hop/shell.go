package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

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

	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		return fmt.Errorf("tunnel to %q is not running; start it with 'hop up'", hostName)
	}

	hostname := state.Hostname
	if hostname == "" {
		hostname = hostName + ".hop"
	}

	sshExtraArgs := []string{"-o", "ConnectTimeout=10"}
	if cfg.SSHKeyPath != "" {
		sshExtraArgs = append(sshExtraArgs, "-i", cfg.SSHKeyPath)
	}

	sshArgs := append([]string{"-t"}, sshExtraArgs...)
	sshArgs = append(sshArgs, user+"@"+hostname)

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
