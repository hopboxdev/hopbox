package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type hopConfig struct {
	Server     string `toml:"server"`
	Port       int    `toml:"port"`
	User       string `toml:"user"`
	DefaultBox string `toml:"default_box"`
}

func defaultConfig() hopConfig {
	return hopConfig{Port: 2222}
}

func loadConfig(path string) (hopConfig, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 2222
	}
	return cfg, nil
}

func (c *hopConfig) applyEnv() {
	if v := os.Getenv("HOP_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("HOP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := os.Getenv("HOP_USER"); v != "" {
		c.User = v
	}
	if v := os.Getenv("HOP_BOX"); v != "" {
		c.DefaultBox = v
	}
}

func (c *hopConfig) sshUser() string {
	if c.DefaultBox != "" {
		return c.User + "+" + c.DefaultBox
	}
	return c.User
}

func (c *hopConfig) sshUserWithBox(box string) string {
	if box != "" {
		return c.User + "+" + box
	}
	return c.sshUser()
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.config/hop/config.toml"
}
