package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hopboxdev/hopbox/internal/devcontainer"
	"gopkg.in/yaml.v3"
)

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct {
	From string `short:"f" help:"Import from devcontainer.json."`
}

func (c *InitCmd) Run() error {
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}

	if c.From != "" {
		return c.importDevcontainer(path)
	}
	return c.scaffold(path)
}

func (c *InitCmd) importDevcontainer(outPath string) error {
	ws, warnings, err := devcontainer.Convert(c.From)
	if err != nil {
		return fmt.Errorf("import devcontainer: %w", err)
	}

	data, err := yaml.Marshal(ws)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	// Prepend warnings as comments.
	var header strings.Builder
	header.WriteString("# Generated from " + c.From + "\n")
	if len(warnings) > 0 {
		header.WriteString("#\n# Warnings (may need manual attention):\n")
		for _, w := range warnings {
			header.WriteString("#   - " + w + "\n")
		}
	}
	header.WriteString("\n")

	output := header.String() + string(data)

	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		return err
	}

	fmt.Printf("Created hopbox.yaml from %s\n", c.From)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
	return nil
}

func (c *InitCmd) scaffold(outPath string) error {
	scaffold := `name: myapp
host: ""

services:
  app:
    type: docker
    image: myapp:latest
    ports: [8080]

bridges:
  - type: clipboard
  # - type: xdg-open
  # - type: notifications

session:
  manager: zellij
  name: myapp
`
	return os.WriteFile(outPath, []byte(scaffold), 0644)
}
