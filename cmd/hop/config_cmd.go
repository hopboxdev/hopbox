package main

import (
	"fmt"
	"os"
	"strings"
)

type ConfigCmd struct{}

func (c *ConfigCmd) Run() error {
	path := configPath()
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}

	var overrides []string
	if os.Getenv("HOP_SERVER") != "" {
		overrides = append(overrides, "HOP_SERVER")
	}
	if os.Getenv("HOP_PORT") != "" {
		overrides = append(overrides, "HOP_PORT")
	}
	if os.Getenv("HOP_USER") != "" {
		overrides = append(overrides, "HOP_USER")
	}
	if os.Getenv("HOP_BOX") != "" {
		overrides = append(overrides, "HOP_BOX")
	}

	cfg.applyEnv()

	fmt.Printf("server:      %s\n", cfg.Server)
	fmt.Printf("port:        %d\n", cfg.Port)
	fmt.Printf("user:        %s\n", cfg.User)
	fmt.Printf("default_box: %s\n", cfg.DefaultBox)

	source := path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		source = "(no config file)"
	}
	if len(overrides) > 0 {
		fmt.Printf("source:      %s (overrides: %s)\n", source, strings.Join(overrides, ", "))
	} else {
		fmt.Printf("source:      %s\n", source)
	}

	return nil
}
