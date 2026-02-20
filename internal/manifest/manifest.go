package manifest

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Workspace is the top-level structure of a hopbox.yaml file.
type Workspace struct {
	Name     string             `yaml:"name"`
	Host     string             `yaml:"host,omitempty"`
	Packages []Package          `yaml:"packages,omitempty"`
	Services map[string]Service `yaml:"services,omitempty"`
	Bridges  []Bridge           `yaml:"bridges,omitempty"`
	Env      map[string]string  `yaml:"env,omitempty"`
	Secrets  []Secret           `yaml:"secrets,omitempty"`
	Scripts  map[string]string  `yaml:"scripts,omitempty"`
	Backup   *BackupConfig      `yaml:"backup,omitempty"`
	Editor   *EditorConfig      `yaml:"editor,omitempty"`
	Session  *SessionConfig     `yaml:"session,omitempty"`
}

// Package declares a system package to install on the remote host.
type Package struct {
	Name    string `yaml:"name"`
	Backend string `yaml:"backend,omitempty"` // "nix", "apt", "static"
	Version string `yaml:"version,omitempty"`
}

// Service declares a background process managed by the agent.
type Service struct {
	Type      string            `yaml:"type"`              // "docker", "kubernetes", "native"
	Image     string            `yaml:"image,omitempty"`   // docker image
	Command   string            `yaml:"command,omitempty"` // native command
	Ports     []string          `yaml:"ports,omitempty"`   // "8080" or "8080:80" (host:container)
	Env       map[string]string `yaml:"env,omitempty"`
	Health    *HealthCheck      `yaml:"health,omitempty"`
	Data      []DataMount       `yaml:"data,omitempty"`
	DependsOn []string          `yaml:"depends_on,omitempty"`
}

// HealthCheck configures how the agent verifies a service is ready.
type HealthCheck struct {
	HTTP     string `yaml:"http,omitempty"`     // URL to poll
	Interval string `yaml:"interval,omitempty"` // e.g. "5s"
	Timeout  string `yaml:"timeout,omitempty"`  // e.g. "30s"
}

// DataMount maps a host path to a container path.
type DataMount struct {
	Host      string `yaml:"host"`
	Container string `yaml:"container"`
}

// Bridge declares a local↔remote resource bridge.
type Bridge struct {
	Type string `yaml:"type"` // "clipboard", "cdp", "xdg-open", "notifications"
}

// Secret declares a secret that should be available in the workspace.
type Secret struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source,omitempty"` // e.g. "1password", "env"
	EnvVar string `yaml:"env,omitempty"`
}

// BackupConfig configures workspace snapshots.
type BackupConfig struct {
	Backend  string `yaml:"backend"` // "restic"
	Target   string `yaml:"target"`  // e.g. "s3://bucket/path"
	Schedule string `yaml:"schedule,omitempty"`
}

// SessionConfig configures the remote terminal session manager.
type SessionConfig struct {
	Manager string `yaml:"manager"` // "zellij", "tmux"
	Name    string `yaml:"name,omitempty"`
}

// EditorConfig configures the remote editor.
type EditorConfig struct {
	Type       string   `yaml:"type"`                 // "vscode-remote"
	Path       string   `yaml:"path,omitempty"`       // remote workspace path
	Extensions []string `yaml:"extensions,omitempty"` // VS Code extension IDs
}

// Parse reads and parses a hopbox.yaml file.
func Parse(path string) (*Workspace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}
	return ParseBytes(data)
}

// ParseBytes parses hopbox.yaml from raw bytes.
func ParseBytes(data []byte) (*Workspace, error) {
	var ws Workspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := ws.Validate(); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Validate checks that required fields are set.
func (w *Workspace) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	for name, svc := range w.Services {
		if svc.Type == "" {
			return fmt.Errorf("service %q: type is required", name)
		}
		switch svc.Type {
		case "docker", "kubernetes", "native":
			// valid
		default:
			return fmt.Errorf("service %q: unknown type %q", name, svc.Type)
		}
	}
	return nil
}
