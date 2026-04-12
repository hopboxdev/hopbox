package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type InitCmd struct{}

func (c *InitCmd) Run() error {
	reader := bufio.NewReader(os.Stdin)

	existing, _ := loadConfig(configPath())

	fmt.Print("Server hostname: ")
	if existing.Server != "" {
		fmt.Printf("[%s]: ", existing.Server)
	}
	server := readLine(reader)
	if server == "" {
		server = existing.Server
	}

	fmt.Printf("SSH port [%d]: ", existing.Port)
	portStr := readLine(reader)
	port := existing.Port
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	fmt.Print("Default box: ")
	if existing.DefaultBox != "" {
		fmt.Printf("[%s]: ", existing.DefaultBox)
	}
	box := readLine(reader)
	if box == "" {
		box = existing.DefaultBox
	}

	cfg := hopConfig{
		Server:     server,
		Port:       port,
		DefaultBox: box,
	}

	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\nConfig written to %s\n", path)
	return nil
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
