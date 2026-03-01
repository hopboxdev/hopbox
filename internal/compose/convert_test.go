package compose

import (
	"testing"
)

func TestConvertBasicCompose(t *testing.T) {
	input := []byte(`
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    depends_on:
      - db
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
`)
	ws, warnings, err := Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(ws.Services))
	}
	if ws.Services["web"].Image != "nginx:latest" {
		t.Fatal("wrong image for web")
	}
	if len(ws.Services["web"].DependsOn) != 1 || ws.Services["web"].DependsOn[0] != "db" {
		t.Fatal("wrong depends_on for web")
	}
	if ws.Services["db"].Env["POSTGRES_PASSWORD"] != "secret" {
		t.Fatal("wrong env for db")
	}
	// Named volume produces a warning.
	if len(warnings) == 0 {
		t.Fatal("expected warnings for named volume")
	}
}

func TestConvertHealthcheck(t *testing.T) {
	input := []byte(`
services:
  api:
    image: myapi:latest
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
`)
	ws, _, err := Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	svc := ws.Services["api"]
	if svc.Health == nil {
		t.Fatal("expected health check")
	}
	if svc.Health.Exec != "curl -f http://localhost:8080/health" {
		t.Fatalf("wrong health exec: %q", svc.Health.Exec)
	}
	if svc.Health.Interval != "10s" {
		t.Fatalf("wrong interval: %q", svc.Health.Interval)
	}
}

func TestConvertHostVolume(t *testing.T) {
	input := []byte(`
services:
  app:
    image: myapp:latest
    volumes:
      - ./data:/app/data
`)
	ws, warnings, err := Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for host path volume, got: %v", warnings)
	}
	svc := ws.Services["app"]
	if len(svc.Data) != 1 {
		t.Fatalf("expected 1 data mount, got %d", len(svc.Data))
	}
	if svc.Data[0].Host != "./data" || svc.Data[0].Container != "/app/data" {
		t.Fatalf("wrong data mount: %+v", svc.Data[0])
	}
}

func TestConvertEnvList(t *testing.T) {
	input := []byte(`
services:
  app:
    image: myapp:latest
    environment:
      - FOO=bar
      - BAZ=qux
`)
	ws, _, err := Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	svc := ws.Services["app"]
	if svc.Env["FOO"] != "bar" || svc.Env["BAZ"] != "qux" {
		t.Fatalf("wrong env: %v", svc.Env)
	}
}

func TestConvertEmptyFile(t *testing.T) {
	_, _, err := Convert([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestConvertDependsOnMap(t *testing.T) {
	input := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_started
`)
	ws, _, err := Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	deps := ws.Services["web"].DependsOn
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}
}
