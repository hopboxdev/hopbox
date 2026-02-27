---
sidebar_position: 3
---

# Services

Hopbox supports two types of services: **Docker containers** and **native processes**. Both are declared in `hopbox.yaml` and managed by the agent.

## Docker services

```yaml
services:
  postgres:
    type: docker
    image: postgres:16
    ports:
      - "5432"
    env:
      POSTGRES_PASSWORD: dev
    data:
      - host: /opt/hopbox/data/postgres
        container: /var/lib/postgresql/data
    health:
      http: http://10.10.0.2:5432/
      interval: 5s
      timeout: 60s
```

Docker services use `docker run` under the hood with `--restart unless-stopped`. The container name matches the service name.

## Native services

```yaml
services:
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
```

Native services run as supervised shell processes. If a native service crashes, the agent restarts it with exponential backoff (1 second up to 30 seconds). The backoff resets if the process runs for at least 60 seconds.

Process output is logged to `~/.config/hopbox/logs/<name>.log`.

## Port binding

By default, service ports are bound to the WireGuard IP (`10.10.0.2`), keeping them private to the tunnel.

| Format | Bind address | Accessible from |
|--------|-------------|-----------------|
| `"8080"` | `10.10.0.2:8080` | Tunnel only |
| `"8080:80"` | `10.10.0.2:8080` | Tunnel only |
| `"0.0.0.0:8080:80"` | All interfaces | Public internet |

Use the 3-part format with an explicit `0.0.0.0` bind address only when you intentionally want to expose a port publicly.

## Health checks

Health checks poll an HTTP endpoint to determine when a service is ready:

```yaml
health:
  http: http://10.10.0.2:8080/health
  interval: 2s
  timeout: 60s
```

| Field | Default | Description |
|-------|---------|-------------|
| `http` | — | URL to poll |
| `interval` | `2s` | Time between polls |
| `timeout` | `60s` | Max time to wait for healthy |

The service is considered ready when the health endpoint returns an HTTP 200. Other services that depend on it (via `depends_on`) wait for this check to pass.

## Dependencies

Use `depends_on` to control startup order:

```yaml
services:
  postgres:
    type: docker
    image: postgres:16
    ports: ["5432"]

  api:
    type: native
    command: go run ./cmd/api
    depends_on:
      - postgres
```

Services are started in topological order — `postgres` starts and passes its health check before `api` starts. On shutdown, services stop in reverse dependency order.

Circular dependencies are detected and reported as an error.

## Data mounts (Docker)

Map host directories into containers for persistent storage:

```yaml
data:
  - host: /opt/hopbox/data/postgres
    container: /var/lib/postgresql/data
```

Data directories are included in snapshots when you run `hop snap create`.

## Managing services

```bash
# List running services
hop services ls

# Restart a service
hop services restart postgres

# Stop a service
hop services stop api

# Stream logs
hop logs postgres
```

`hop services ls` only shows services declared in `hopbox.yaml`. Docker containers running independently on the host are not visible.
