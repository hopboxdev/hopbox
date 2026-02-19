package manifest_test

import (
	"testing"

	"github.com/hopboxdev/hopbox/internal/manifest"
)

const exampleYAML = `
name: myapp
packages:
  - name: postgresql
    backend: apt
  - name: redis
    backend: apt

services:
  postgres:
    type: docker
    image: postgres:16
    ports: [5432]
    env:
      POSTGRES_PASSWORD: secret
    health:
      http: http://localhost:5432
      interval: 5s
      timeout: 30s
  api:
    type: native
    command: ./bin/server
    ports: [8080]
    depends_on: [postgres]

bridges:
  - type: clipboard
  - type: cdp

env:
  DATABASE_URL: postgres://localhost/myapp

scripts:
  migrate: go run ./cmd/migrate
  seed: go run ./cmd/seed

backup:
  backend: restic
  target: s3://mybucket/myapp

editor: nvim

session:
  manager: zellij
  name: myapp
`

func TestParseExampleYAML(t *testing.T) {
	ws, err := manifest.ParseBytes([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	if ws.Name != "myapp" {
		t.Errorf("Name = %q, want %q", ws.Name, "myapp")
	}
	if len(ws.Packages) != 2 {
		t.Errorf("Packages len = %d, want 2", len(ws.Packages))
	}
	if len(ws.Services) != 2 {
		t.Errorf("Services len = %d, want 2", len(ws.Services))
	}

	postgres, ok := ws.Services["postgres"]
	if !ok {
		t.Fatal("missing service 'postgres'")
	}
	if postgres.Type != "docker" {
		t.Errorf("postgres.Type = %q, want docker", postgres.Type)
	}
	if postgres.Image != "postgres:16" {
		t.Errorf("postgres.Image = %q, want postgres:16", postgres.Image)
	}

	api, ok := ws.Services["api"]
	if !ok {
		t.Fatal("missing service 'api'")
	}
	if api.Type != "native" {
		t.Errorf("api.Type = %q, want native", api.Type)
	}
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "postgres" {
		t.Errorf("api.DependsOn = %v, want [postgres]", api.DependsOn)
	}

	if len(ws.Bridges) != 2 {
		t.Errorf("Bridges len = %d, want 2", len(ws.Bridges))
	}
	if ws.Editor != "nvim" {
		t.Errorf("Editor = %q, want nvim", ws.Editor)
	}
	if ws.Session == nil || ws.Session.Manager != "zellij" {
		t.Error("Session.Manager should be zellij")
	}
	if ws.Backup == nil || ws.Backup.Target != "s3://mybucket/myapp" {
		t.Error("Backup.Target mismatch")
	}
	if migrate, ok := ws.Scripts["migrate"]; !ok || migrate != "go run ./cmd/migrate" {
		t.Errorf("Scripts[migrate] = %q, want 'go run ./cmd/migrate'", ws.Scripts["migrate"])
	}
}

func TestValidateMissingName(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`services: {}`))
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestValidateUnknownServiceType(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`
name: test
services:
  svc:
    type: kubernetes_custom
`))
	if err == nil {
		t.Error("expected error for unknown service type")
	}
}

func TestValidateMissingServiceType(t *testing.T) {
	_, err := manifest.ParseBytes([]byte(`
name: test
services:
  svc:
    image: nginx
`))
	if err == nil {
		t.Error("expected error for missing service type")
	}
}
