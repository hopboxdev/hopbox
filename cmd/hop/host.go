package main

import (
	"fmt"
	"strings"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// HostCmd manages the host registry.
type HostCmd struct {
	Add     HostAddCmd     `cmd:"" name:"add" help:"Add a host."`
	Rm      HostRmCmd      `cmd:"" name:"rm" help:"Remove a host."`
	Ls      HostLsCmd      `cmd:"" name:"ls" help:"List hosts."`
	Default HostDefaultCmd `cmd:"" name:"default" help:"Get or set the default host."`
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
	if err := hostconfig.Delete(c.Name); err != nil {
		return err
	}
	// Clear default_host if it pointed to the removed host.
	if cfg, err := hostconfig.LoadGlobalConfig(); err == nil && cfg.DefaultHost == c.Name {
		cfg.DefaultHost = ""
		_ = cfg.Save()
	}
	return nil
}

// HostLsCmd lists registered hosts.
type HostLsCmd struct{}

func (c *HostLsCmd) Run() error {
	names, err := hostconfig.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println(ui.Section("Hosts", "No hosts configured. Use 'hop setup' to add one.", ui.MaxWidth))
		return nil
	}
	cfg, _ := hostconfig.LoadGlobalConfig()
	var lines []string
	for _, n := range names {
		if cfg != nil && n == cfg.DefaultHost {
			lines = append(lines, fmt.Sprintf("%s %s   (default)", ui.Dot(ui.StateConnected), n))
		} else {
			lines = append(lines, "  "+n)
		}
	}
	fmt.Println(ui.Section("Hosts", strings.Join(lines, "\n"), ui.MaxWidth))
	return nil
}

// HostDefaultCmd gets or sets the default host.
type HostDefaultCmd struct {
	Name string `arg:"" optional:"" help:"Host name to set as default. If omitted, prints the current default."`
}

func (c *HostDefaultCmd) Run() error {
	if c.Name == "" {
		cfg, err := hostconfig.LoadGlobalConfig()
		if err != nil {
			return err
		}
		if cfg.DefaultHost == "" {
			fmt.Println("No default host set. Run 'hop host default <name>' to set one.")
		} else {
			fmt.Println(cfg.DefaultHost)
		}
		return nil
	}
	if _, err := hostconfig.Load(c.Name); err != nil {
		return fmt.Errorf("host %q not found: run 'hop setup %s --addr <ip>' first", c.Name, c.Name)
	}
	if err := hostconfig.SetDefaultHost(c.Name); err != nil {
		return err
	}
	fmt.Printf("Default host set to %q.\n", c.Name)
	return nil
}
