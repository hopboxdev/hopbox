package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

type SSHCmd struct {
	Box string `help:"Box to connect to (overrides default)." short:"b"`
}

func (c *SSHCmd) Run() error {
	cfg, err := resolveConfig()
	if err != nil {
		return err
	}

	sshUser := cfg.sshUserWithBox(c.Box)

	args := []string{
		"-p", strconv.Itoa(cfg.Port),
		sshUser + "@" + cfg.Server,
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveConfig() (*hopConfig, error) {
	cfg, err := loadConfig(configPath())
	if err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if cfg.Server == "" {
		return nil, fmt.Errorf("not configured — run `hop init` first")
	}
	return &cfg, nil
}
