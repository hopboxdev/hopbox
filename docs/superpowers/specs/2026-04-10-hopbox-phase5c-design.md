# Hopbox Phase 5C Design — Observability: Logging, Health, Metrics, Dashboards

## Overview

Phase 5C adds production observability: structured logging via `log/slog`, a `/healthz` endpoint for uptime monitoring, Prometheus metrics (business + per-box resource usage), Grafana dashboards, and a Docker Compose stack to deploy Prometheus + Grafana alongside hopboxd.

**Goal:** Operators can monitor hopbox in production with standard tools. `/metrics` exposes business and per-container metrics, `/healthz` enables uptime checks, JSON logs integrate with log aggregators, and two Grafana dashboards give server and per-box views.

## Structured Logging

Replace all `log.Printf` calls with `log/slog` from the standard library. Format and level are configurable.

**Config additions:**
```toml
log_format = "text"  # "text" | "json"
log_level  = "info"  # "debug" | "info" | "warn" | "error"
```

Defaults: text format, info level.

**Initialization in main.go:**
```go
var handler slog.Handler
opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}
if cfg.LogFormat == "json" {
    handler = slog.NewJSONHandler(os.Stderr, opts)
} else {
    handler = slog.NewTextHandler(os.Stderr, opts)
}
slog.SetDefault(slog.New(handler))
```

**Logging pattern:** Replace string-formatted logs with structured fields.

```go
// Before
log.Printf("[auth] user=%s key=%s addr=%s", user.Username, keyType, addr)

// After
slog.Info("auth success", "user", user.Username, "key_type", keyType, "addr", addr)
```

Migration is mechanical — every `log.Printf("[component] ...")` becomes `slog.Info("...", key, value)` with the component as part of the message or a field.

## Health Check

Public endpoint on the admin HTTP server (no auth required — standard monitoring pattern).

**Route:** `GET /healthz`

**Response (healthy):** HTTP 200
```json
{"status": "healthy", "docker": "reachable"}
```

**Response (unhealthy):** HTTP 503
```json
{"status": "unhealthy", "docker": "unreachable", "error": "dial /var/run/docker.sock: connection refused"}
```

**Logic:** Ping Docker with a 2-second timeout. Return 200 if reachable, 503 otherwise. The HTTP status code is the primary signal for uptime checks; the JSON body helps humans debug.

## Prometheus Metrics

**Library:** `github.com/prometheus/client_golang/prometheus` + `promhttp`

**Endpoint:** `GET /metrics` on the admin HTTP server (public, no auth — standard Prometheus pattern). Uses `promhttp.Handler()` which includes Go runtime metrics automatically.

### Business Metrics

```
hopbox_users_total                    # gauge: total registered users
hopbox_boxes_total                    # gauge: total boxes across all users
hopbox_containers_running_total       # gauge: currently running containers
hopbox_active_sessions_total          # gauge: current SSH sessions
hopbox_box_connect_total              # counter: connect events (with labels user, box)
hopbox_build_duration_seconds         # histogram: per-user image build times
```

### Per-Box Resource Metrics

Labels: `{user, box, container_id}`

```
hopbox_box_cpu_percent                # gauge: CPU usage percentage
hopbox_box_memory_bytes               # gauge: current memory usage
hopbox_box_memory_limit_bytes         # gauge: memory limit
hopbox_box_network_rx_bytes           # counter: total received bytes
hopbox_box_network_tx_bytes           # counter: total sent bytes
hopbox_box_block_read_bytes           # counter: total disk read bytes
hopbox_box_block_write_bytes          # counter: total disk write bytes
```

### Stats Collector

A background goroutine polls Docker stats every 10 seconds and updates the per-box gauges/counters. Lives in `internal/metrics/collector.go`.

**Flow:**
1. List all containers with name prefix `hopbox-`
2. For each running container, fetch stats via `cli.ContainerStats(ctx, id, false)` (non-streaming, one-shot)
3. Parse the stats JSON
4. Update gauges with the calculated values
5. Remove metrics for containers that no longer exist (to avoid stale labels)

Polling interval is 10 seconds — fresh enough for Grafana, lightweight on Docker.

**Label management:** When a container is removed, delete its metrics using `gauge.DeleteLabelValues(user, box, containerID)` to prevent stale metric accumulation.

## Grafana Dashboards

Two JSON dashboards in `deploy/grafana/`:

### Server Overview (`server-overview.json`)

- **Stat row:** total users, total boxes, running containers, active sessions
- **Time series:** active sessions over time
- **Time series:** build duration histogram (p50, p95, p99)
- **Table:** running containers with user, box, container_id labels
- **Go runtime row:** goroutines, memory, GC pauses

### Box Details (`box-details.json`)

- **Template variables:** `$user` and `$box` (populated from metric label values)
- **Stat row:** current CPU %, current memory, container ID
- **Time series:** CPU usage over time
- **Time series:** memory usage (current vs limit)
- **Time series:** network I/O (rx/tx rates)
- **Time series:** disk I/O (read/write rates)

Users import these dashboards into Grafana via the UI (Dashboards → Import → Upload JSON).

## Monitoring Stack Deployment

Docker Compose file to run Prometheus + Grafana alongside a host-running hopboxd.

### `deploy/monitoring/compose.yml`

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    container_name: hopbox-prometheus
    restart: unless-stopped
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    extra_hosts:
      - "host.docker.internal:host-gateway"

  grafana:
    image: grafana/grafana:latest
    container_name: hopbox-grafana
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

### `deploy/monitoring/prometheus.yml`

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'hopbox'
    static_configs:
      - targets: ['host.docker.internal:8080']
```

The `host.docker.internal` hostname lets the Prometheus container reach hopboxd running on the host.

### `deploy/monitoring/README.md`

Setup instructions:
1. Enable admin server in hopbox config (port 8080)
2. `cd deploy/monitoring && docker compose up -d`
3. Open Grafana at http://localhost:3000 (admin/admin)
4. Add Prometheus datasource: `http://prometheus:9090`
5. Import dashboards from `deploy/grafana/`

## File Changes Summary

**New files:**
```
internal/metrics/
├── metrics.go              # Prometheus metric definitions
└── collector.go            # Docker stats poller
deploy/monitoring/
├── compose.yml             # Prometheus + Grafana stack
├── prometheus.yml          # Prometheus scrape config
└── README.md               # Setup instructions
deploy/grafana/
├── server-overview.json    # Server overview dashboard
├── box-details.json        # Per-box details dashboard
└── README.md               # Import instructions
```

**Modified files:**
```
internal/config/config.go              # add LogFormat, LogLevel
cmd/hopboxd/main.go                    # init slog, start metrics collector
internal/admin/server.go               # add /healthz and /metrics (public routes)
internal/containers/manager.go         # update business metrics on lifecycle events
internal/gateway/server.go             # replace log.Printf with slog, track metrics
internal/gateway/tunnel.go             # replace log.Printf with slog
internal/containers/image.go           # replace log.Printf with slog
internal/containers/builder.go         # replace log.Printf with slog, build duration metric
internal/control/socket.go             # replace log.Printf with slog
internal/control/handler.go            # replace log.Printf with slog
internal/users/store.go                # replace log.Printf with slog
```

## What Phase 5C Does NOT Include

- Log shipping (Loki, Elasticsearch) — operators can pipe JSON logs to any collector
- Alerting rules (users define in Grafana/Prometheus)
- Per-user aggregate metrics (can be derived from per-box metrics in PromQL)
- Separate dashboards per box profile
