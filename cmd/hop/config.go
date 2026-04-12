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
	DefaultBox string `toml:"default_box"`
}

func defaultConfig() hopConfig {
	return hopConfig{Server: "hopbox.dev", Port: 2222, DefaultBox: "default"}
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
	if v := os.Getenv("HOP_BOX"); v != "" {
		c.DefaultBox = v
	}
}

// sshUser returns the SSH username for the connection.
// Always "hop" with optional "+boxname" for routing.
func (c *hopConfig) sshUser() string {
	if c.DefaultBox != "" {
		return "hop+" + c.DefaultBox
	}
	return "hop"
}

func (c *hopConfig) sshUserWithBox(box string) string {
	if box != "" {
		return "hop+" + box
	}
	return c.sshUser()
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.config/hopbox/config.toml"
}
