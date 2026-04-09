package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 2222 {
		t.Errorf("expected port 2222, got %d", cfg.Port)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected data dir ./data, got %s", cfg.DataDir)
	}
	if cfg.HostKeyPath != "" {
		t.Errorf("expected empty host key path, got %s", cfg.HostKeyPath)
	}
	if !cfg.OpenRegistration {
		t.Error("expected open registration to be true by default")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`
port = 3333
data_dir = "/tmp/hopbox"
host_key_path = "/etc/hopbox/key"
open_registration = false
`), 0644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3333 {
		t.Errorf("expected port 3333, got %d", cfg.Port)
	}
	if cfg.DataDir != "/tmp/hopbox" {
		t.Errorf("expected data dir /tmp/hopbox, got %s", cfg.DataDir)
	}
	if cfg.HostKeyPath != "/etc/hopbox/key" {
		t.Errorf("expected host key path /etc/hopbox/key, got %s", cfg.HostKeyPath)
	}
	if cfg.OpenRegistration {
		t.Error("expected open registration to be false")
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/config.toml")
	if err != nil {
		t.Fatalf("missing file should return defaults, got error: %v", err)
	}
	if cfg.Port != 2222 {
		t.Errorf("expected default port, got %d", cfg.Port)
	}
}

func TestLoadResourceDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeoutHours != 24 {
		t.Errorf("idle timeout: got %d, want 24", cfg.IdleTimeoutHours)
	}
	if cfg.Resources.CPUCores != 2 {
		t.Errorf("cpu cores: got %d, want 2", cfg.Resources.CPUCores)
	}
	if cfg.Resources.MemoryGB != 4 {
		t.Errorf("memory gb: got %d, want 4", cfg.Resources.MemoryGB)
	}
	if cfg.Resources.PidsLimit != 512 {
		t.Errorf("pids limit: got %d, want 512", cfg.Resources.PidsLimit)
	}
}

func TestLoadResourcesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`
port = 2222
idle_timeout_hours = 12

[resources]
cpu_cores = 4
memory_gb = 8
pids_limit = 1024
`), 0644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IdleTimeoutHours != 12 {
		t.Errorf("idle timeout: got %d, want 12", cfg.IdleTimeoutHours)
	}
	if cfg.Resources.CPUCores != 4 {
		t.Errorf("cpu cores: got %d, want 4", cfg.Resources.CPUCores)
	}
	if cfg.Resources.MemoryGB != 8 {
		t.Errorf("memory gb: got %d, want 8", cfg.Resources.MemoryGB)
	}
	if cfg.Resources.PidsLimit != 1024 {
		t.Errorf("pids limit: got %d, want 1024", cfg.Resources.PidsLimit)
	}
}
