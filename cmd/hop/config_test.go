package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
server = "hopbox.dev"
port = 2222
default_box = "main"
`), 0644)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "hopbox.dev" {
		t.Errorf("server = %q, want %q", cfg.Server, "hopbox.dev")
	}
	if cfg.Port != 2222 {
		t.Errorf("port = %d, want %d", cfg.Port, 2222)
	}
	if cfg.DefaultBox != "main" {
		t.Errorf("default_box = %q, want %q", cfg.DefaultBox, "main")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := loadConfig("/nonexistent/config.toml")
	if err != nil {
		t.Fatal("missing file should not error, just return defaults")
	}
	if cfg.Port != 2222 {
		t.Errorf("default port = %d, want 2222", cfg.Port)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
server = "hopbox.dev"
port = 2222
default_box = "main"
`), 0644)

	t.Setenv("HOP_SERVER", "other.dev")
	t.Setenv("HOP_BOX", "work")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.applyEnv()

	if cfg.Server != "other.dev" {
		t.Errorf("server = %q, want %q", cfg.Server, "other.dev")
	}
	if cfg.DefaultBox != "work" {
		t.Errorf("default_box = %q, want %q", cfg.DefaultBox, "work")
	}
}

func TestSSHUser(t *testing.T) {
	cfg := hopConfig{DefaultBox: "main"}
	if got := cfg.sshUser(); got != "hop+main" {
		t.Errorf("sshUser() = %q, want %q", got, "hop+main")
	}
}

func TestSSHUserNoBox(t *testing.T) {
	cfg := hopConfig{}
	if got := cfg.sshUser(); got != "hop" {
		t.Errorf("sshUser() = %q, want %q", got, "hop")
	}
}

func TestSSHUserWithBoxOverride(t *testing.T) {
	cfg := hopConfig{DefaultBox: "main"}
	if got := cfg.sshUserWithBox("work"); got != "hop+work" {
		t.Errorf("sshUserWithBox(\"work\") = %q, want %q", got, "hop+work")
	}
}
