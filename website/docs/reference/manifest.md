---
sidebar_position: 2
---

# Manifest Reference

The `hopbox.yaml` file declares everything about a workspace: packages, services, bridges, environment, scripts, backups, and editor configuration.

## Top-level fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Workspace identifier |
| `host` | string | no | Pin to a specific registered host |
| `packages` | Package[] | no | System packages to install |
| `services` | map[string]Service | no | Background services |
| `bridges` | Bridge[] | no | Local-remote resource bridges |
| `env` | map[string]string | no | Environment variables |
| `scripts` | map[string]string | no | Named shell scripts |
| `backup` | BackupConfig | no | Snapshot configuration |
| `editor` | EditorConfig | no | Editor integration |
| `session` | SessionConfig | no | Terminal session manager |

## Package

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Package name |
| `backend` | string | no | `"nix"`, `"apt"`, or `"static"` |
| `version` | string | no | Version constraint |
| `url` | string | no | Download URL (required for `static` backend) |
| `sha256` | string | no | Hex-encoded SHA256 checksum |

```yaml
packages:
  - name: nodejs
    backend: nix

  - name: postgresql
    backend: apt

  - name: ripgrep
    backend: static
    url: https://github.com/BurntSushi/ripgrep/releases/download/14.0.0/ripgrep-14.0.0-x86_64-unknown-linux-musl.tar.gz
    sha256: abc123...
```

## Service

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | `"docker"` or `"native"` |
| `image` | string | no | Docker image (docker only) |
| `command` | string | no | Shell command (required for native) |
| `workdir` | string | no | Working directory (native only) |
| `ports` | string[] | no | Port mappings |
| `env` | map[string]string | no | Environment variables |
| `health` | HealthCheck | no | Readiness check |
| `data` | DataMount[] | no | Volume mounts (docker only) |
| `depends_on` | string[] | no | Service dependencies |

### Port format

| Format | Bind address | Visibility |
|--------|-------------|------------|
| `"8080"` | `10.10.0.2:8080` | Tunnel only |
| `"8080:80"` | `10.10.0.2:8080` | Tunnel only |
| `"0.0.0.0:8080:80"` | All interfaces | Public |

### HealthCheck

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `http` | string | — | URL to poll for readiness |
| `interval` | string | `"2s"` | Time between polls |
| `timeout` | string | `"60s"` | Max wait time |

### DataMount

| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Host filesystem path |
| `container` | string | Container mount path |

```yaml
services:
  postgres:
    type: docker
    image: postgres:16
    ports:
      - "5432"
    env:
      POSTGRES_PASSWORD: dev
    health:
      http: http://10.10.0.2:5432/
      interval: 5s
      timeout: 60s
    data:
      - host: /opt/hopbox/data/postgres
        container: /var/lib/postgresql/data

  api:
    type: native
    command: go run ./cmd/api
    workdir: /home/debian/project
    ports:
      - "8080"
    depends_on:
      - postgres
```

## Bridge

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `"clipboard"`, `"cdp"`, `"xdg-open"`, or `"notifications"` |

```yaml
bridges:
  - type: clipboard
  - type: cdp
  - type: xdg-open
  - type: notifications
```

## BackupConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `backend` | string | yes | `"restic"` |
| `target` | string | yes | Restic repository URL |
| `schedule` | string | no | Backup schedule |

```yaml
backup:
  backend: restic
  target: s3:s3.amazonaws.com/mybucket/workspace
```

## SessionConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `manager` | string | yes | `"zellij"` or `"tmux"` |
| `name` | string | no | Session name |

```yaml
session:
  manager: zellij
  name: myproject
```

## EditorConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | `"vscode-remote"` |
| `path` | string | no | Remote workspace path |
| `extensions` | string[] | no | VS Code extension IDs |

```yaml
editor:
  type: vscode-remote
  path: /home/debian/project
  extensions:
    - golang.go
    - ms-python.python
```

## Complete example

```yaml
name: myproject
host: mybox

packages:
  - name: nodejs
    backend: nix
  - name: postgresql
    backend: apt

services:
  postgres:
    type: docker
    image: postgres:16
    ports:
      - "5432"
    env:
      POSTGRES_PASSWORD: dev
    health:
      http: http://10.10.0.2:5432/
      interval: 5s
      timeout: 60s
    data:
      - host: /opt/hopbox/data/postgres
        container: /var/lib/postgresql/data

  api:
    type: native
    command: go run ./cmd/api
    workdir: /home/debian/project
    ports:
      - "8080"
    env:
      DATABASE_URL: postgres://localhost:5432/dev
    depends_on:
      - postgres

bridges:
  - type: clipboard
  - type: xdg-open

env:
  NODE_ENV: development

scripts:
  build: cd /home/debian/project && go build ./...
  test: cd /home/debian/project && go test ./...

backup:
  backend: restic
  target: s3:s3.amazonaws.com/mybucket/workspace

session:
  manager: zellij
  name: myproject

editor:
  type: vscode-remote
  path: /home/debian/project
  extensions:
    - golang.go
```
