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

func (c *SSHCmd) Run(cfg ...*hopConfig) error {
	conf, err := resolveConfig(cfg)
	if err != nil {
		return err
	}

	sshUser := conf.sshUserWithBox(c.Box)

	args := []string{
		"-p", strconv.Itoa(conf.Port),
		sshUser + "@" + conf.Server,
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveConfig(passed []*hopConfig) (*hopConfig, error) {
	if len(passed) > 0 && passed[0] != nil {
		return passed[0], nil
	}
	cfg, err := loadConfig(configPath())
	if err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if cfg.Server == "" || cfg.User == "" {
		return nil, fmt.Errorf("not configured — run `hop init` first")
	}
	return &cfg, nil
}
