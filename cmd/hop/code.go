package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// CodeCmd opens VS Code connected to the workspace on the VPS.
type CodeCmd struct {
	Path string `arg:"" optional:"" help:"Remote workspace path (overrides editor.path in hopbox.yaml)."`
}

func (c *CodeCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		return fmt.Errorf("tunnel to %q is not running; start it with 'hop up'", hostName)
	}

	hostname := state.Hostname
	if hostname == "" {
		hostname = hostName + ".hop"
	}

	user := cfg.SSHUser
	if user == "" {
		user = "root"
	}

	// Write managed SSH config entry.
	if err := writeSSHConfig(hostname, user, cfg.SSHKeyPath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: could not update SSH config: %v\n", err)
	}

	// Resolve workspace path.
	wsPath := c.Path
	if wsPath == "" {
		if ws, err := manifest.Parse("hopbox.yaml"); err == nil && ws.Editor != nil {
			wsPath = ws.Editor.Path
		}
	}
	if wsPath == "" {
		wsPath = "/root"
		if user != "root" {
			wsPath = "/home/" + user
		}
	}

	// Launch VS Code.
	remote := fmt.Sprintf("ssh-remote+%s", hostname)
	fmt.Printf("Opening VS Code: %s:%s\n", hostname, wsPath)
	cmd := exec.Command("code", "--remote", remote, wsPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

const (
	sshMarkerStart = "# --- hopbox managed start ---"
	sshMarkerEnd   = "# --- hopbox managed end ---"
)

func writeSSHConfig(hostname, user, keyPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}
	configPath := filepath.Join(sshDir, "config")

	var content string
	if data, err := os.ReadFile(configPath); err == nil {
		content = string(data)
	}

	// Build the entry.
	var entry strings.Builder
	fmt.Fprintf(&entry, "Host %s\n", hostname)
	fmt.Fprintf(&entry, "  HostName %s\n", hostname)
	fmt.Fprintf(&entry, "  User %s\n", user)
	if keyPath != "" {
		fmt.Fprintf(&entry, "  IdentityFile %s\n", keyPath)
	}

	// Remove existing managed section.
	startIdx := strings.Index(content, sshMarkerStart)
	endIdx := strings.Index(content, sshMarkerEnd)
	if startIdx != -1 && endIdx != -1 {
		// Extract entries for OTHER hosts, keep them.
		sectionStart := startIdx + len(sshMarkerStart) + 1
		section := content[sectionStart:endIdx]
		var kept []string
		for _, block := range splitHostBlocks(section) {
			blockHostname := extractHostname(block)
			if blockHostname != "" && blockHostname != hostname {
				kept = append(kept, block)
			}
		}
		kept = append(kept, entry.String())
		newSection := strings.Join(kept, "")
		content = content[:startIdx] + sshMarkerStart + "\n" + newSection + sshMarkerEnd + "\n"
	} else {
		// Append new managed section.
		content = strings.TrimRight(content, "\n") + "\n\n" +
			sshMarkerStart + "\n" + entry.String() + sshMarkerEnd + "\n"
	}

	return os.WriteFile(configPath, []byte(content), 0600)
}

func splitHostBlocks(section string) []string {
	var blocks []string
	lines := strings.Split(section, "\n")
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "Host ") && len(current) > 0 {
			blocks = append(blocks, strings.Join(current, "\n")+"\n")
			current = nil
		}
		if strings.TrimSpace(line) != "" {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n")+"\n")
	}
	return blocks
}

func extractHostname(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "Host ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Host "))
		}
	}
	return ""
}
