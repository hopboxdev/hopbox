package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hopboxdev/hopbox/internal/compose"
	"github.com/hopboxdev/hopbox/internal/devcontainer"
	"gopkg.in/yaml.v3"
)

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct {
	From        string `short:"f" help:"Import from devcontainer.json."`
	FromCompose string `help:"Import from docker-compose.yml."`
}

func (c *InitCmd) Run() error {
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}

	if c.From != "" {
		return c.importDevcontainer(path)
	}
	if c.FromCompose != "" {
		return c.importCompose(path)
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

func (c *InitCmd) importCompose(outPath string) error {
	data, err := os.ReadFile(c.FromCompose)
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	ws, warnings, err := compose.Convert(data)
	if err != nil {
		return fmt.Errorf("import compose: %w", err)
	}

	yamlData, err := yaml.Marshal(ws)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	var header strings.Builder
	header.WriteString("# Generated from " + c.FromCompose + "\n")
	if len(warnings) > 0 {
		header.WriteString("#\n# Warnings (may need manual attention):\n")
		for _, w := range warnings {
			header.WriteString("#   - " + w + "\n")
		}
	}
	header.WriteString("\n")

	output := header.String() + string(yamlData)

	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		return err
	}

	fmt.Printf("Created hopbox.yaml from %s\n", c.FromCompose)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
	return nil
}

func (c *InitCmd) scaffold(outPath string) error {
	scaffold := `# Workspace name (required)
name: myapp

# Host to use (run 'hop host ls' to see available hosts)
# host: mybox

# System packages installed on the remote host
# packages:
#   - name: nodejs
#     backend: nix
#   - name: ripgrep
#     backend: apt

# Background services
services:
  app:
    type: docker
    image: myapp:latest
    ports: ["8080"]
    # health:
    #   http: http://10.10.0.2:8080/health
    #   timeout: 30s

# Lifecycle hooks (run once after first sync or migration)
# hooks:
#   setup: "npm install && npm run db:migrate"
#   start: "npm run dev"

# Local-remote bridges
bridges:
  - type: clipboard
  # - type: xdg-open
  # - type: notifications

# Editor configuration
# editor:
#   type: vscode-remote
#   path: /home/debian/myapp
#   extensions: [dbaeumer.vscode-eslint]

# Terminal session manager
# session:
#   manager: zellij
#   name: myapp
`
	if err := os.WriteFile(outPath, []byte(scaffold), 0644); err != nil {
		return err
	}
	fmt.Println("Created hopbox.yaml — edit it, then run 'hop up'")
	return nil
}
