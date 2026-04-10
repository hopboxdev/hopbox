# Phase 5C Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add structured logging (`log/slog`), a `/healthz` health check, Prometheus metrics (business + per-box), a Docker-stats collector, and two Grafana dashboards with a Prometheus+Grafana compose stack.

**Architecture:**

- All logging goes through `log/slog`, configured once in `main.go` from `cfg.LogFormat` and `cfg.LogLevel`. JSON output integrates with log aggregators; text output stays readable in dev.
- A new package `internal/metrics` owns Prometheus metric definitions and a Docker-stats `Collector` that polls every 10s and updates per-box gauges/counters, cleaning up stale label sets.
- The admin HTTP server exposes two new **public** routes (`/healthz`, `/metrics`) that bypass basic-auth, alongside the existing authenticated pages. To support this, the auth middleware is reshaped so that it wraps only the page/API sub-mux, not the health/metrics handlers.
- `manager.go`, `builder.go`, and `gateway/server.go` update metrics at lifecycle events (session connect/disconnect, container create/destroy, build timing, connect counter).
- Grafana dashboards and a Prometheus+Grafana compose stack live under `deploy/`.

**Tech Stack:** Go, `log/slog`, `github.com/prometheus/client_golang/prometheus`, `promhttp`, Docker SDK (`ContainerStats`), Grafana, Prometheus.

---

## Task 1: Config — add logging fields

**File:** `internal/config/config.go`

Add `LogFormat` and `LogLevel` fields to `Config` with defaults `"text"` / `"info"`.

```go
type Config struct {
	Port             int             `toml:"port"`
	Hostname         string          `toml:"hostname"`
	DataDir          string          `toml:"data_dir"`
	HostKeyPath      string          `toml:"host_key_path"`
	OpenRegistration bool            `toml:"open_registration"`
	IdleTimeoutHours int             `toml:"idle_timeout_hours"`
	LogFormat        string          `toml:"log_format"`
	LogLevel         string          `toml:"log_level"`
	Resources        ResourcesConfig `toml:"resources"`
	Admin            AdminConfig     `toml:"admin"`
}
```

Update `defaults()`:

```go
func defaults() Config {
	return Config{
		Port:             2222,
		Hostname:         "",
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
		IdleTimeoutHours: 24,
		LogFormat:        "text",
		LogLevel:         "info",
		Resources: ResourcesConfig{
			CPUCores:  2,
			MemoryGB:  4,
			PidsLimit: 512,
		},
		Admin: AdminConfig{
			Enabled:  false,
			Port:     8080,
			Username: "admin",
			Password: "",
		},
	}
}
```

Also add matching entries to `config.example.toml`:

```toml
log_format = "text"   # "text" or "json"
log_level  = "info"   # "debug" | "info" | "warn" | "error"
```

**Tests:** Extend `internal/config/config_test.go` to assert defaults and override parsing of the two new fields.

---

## Task 2: Initialize slog in main.go

**File:** `cmd/hopboxd/main.go`

Add a helper `initLogger(cfg config.Config)` that builds a `slog.Handler` from the config and installs it as the default. Call it immediately after `config.Load` (config loading itself still uses the stdlib `log.Fatalf` for its single failure path, which is fine — it runs before the logger is swapped).

Add imports:

```go
import (
	"log/slog"
	// ...
)
```

Helper:

```go
func initLogger(cfg config.Config) {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}
```

Wire it in:

```go
cfg, err := config.Load(*configPath)
if err != nil {
	log.Fatalf("load config: %v", err)
}
initLogger(cfg)
```

Add `"strings"` to the import block if not already present.

---

## Task 3: Migrate log.Printf to slog

Replace each `log.Printf` (and `log.Println`) with a `slog.Info/Warn/Error` call. The `[component]` prefix becomes a `component` field or stays inside the message as the canonical verb. Remove the unused `"log"` import from files that no longer reference it.

### `cmd/hopboxd/main.go`

```go
// Before
log.Printf("config: port=%d data_dir=%s registration=%v idle_timeout=%dh resources=[cpu=%d mem=%dGB pids=%d]",
	cfg.Port, cfg.DataDir, cfg.OpenRegistration, cfg.IdleTimeoutHours,
	cfg.Resources.CPUCores, cfg.Resources.MemoryGB, cfg.Resources.PidsLimit)
// After
slog.Info("config loaded",
	"port", cfg.Port,
	"data_dir", cfg.DataDir,
	"open_registration", cfg.OpenRegistration,
	"idle_timeout_hours", cfg.IdleTimeoutHours,
	"cpu_cores", cfg.Resources.CPUCores,
	"memory_gb", cfg.Resources.MemoryGB,
	"pids_limit", cfg.Resources.PidsLimit)

// Before
log.Printf("using base image: %s", imageTag)
// After
slog.Info("base image ready", "image", imageTag)

// Before
log.Printf("admin UI: http://0.0.0.0:%d (user: %s)", cfg.Admin.Port, cfg.Admin.Username)
// After
slog.Info("admin UI listening", "port", cfg.Admin.Port, "user", cfg.Admin.Username)

// Before
log.Printf("admin server error: %v", err)
// After
slog.Error("admin server error", "err", err)

// Before
log.Println("shutting down...")
// After
slog.Info("shutting down")

// Before
log.Println("shutdown complete")
// After
slog.Info("shutdown complete")

// Before
log.Printf("server stopped: %v", err)
// After
slog.Error("server stopped", "err", err)
```

Leave the `log.Fatalf` calls for pre-logger initialization failures (they already call `os.Exit(1)` and run before `initLogger`). For the remaining `log.Fatalf` calls after logger init, convert to:

```go
slog.Error("ensure base image failed", "err", err)
os.Exit(1)
```

### `internal/gateway/server.go`

```go
// Before
log.Printf("hopboxd listening on :%d", s.cfg.Port)
// After
slog.Info("hopboxd listening", "port", s.cfg.Port)

// Before
log.Printf("[auth] user=%s key=%s addr=%s", user.Username, key.Type(), ctx.RemoteAddr())
// After
slog.Info("auth success", "component", "auth", "user", user.Username, "key_type", key.Type(), "addr", ctx.RemoteAddr())

// Before
log.Printf("[auth] new key addr=%s type=%s — starting registration", ctx.RemoteAddr(), key.Type())
// After
slog.Info("auth new key, starting registration", "component", "auth", "addr", ctx.RemoteAddr(), "key_type", key.Type())

// Before
log.Printf("[auth] rejected unknown key addr=%s (registration closed)", ctx.RemoteAddr())
// After
slog.Warn("auth rejected: registration closed", "component", "auth", "addr", ctx.RemoteAddr())

// Before
log.Printf("[session] picker cancelled for user=%s: %v", user.Username, err)
// After
slog.Warn("picker cancelled", "component", "session", "user", user.Username, "err", err)

// Before
log.Printf("[session] user=%s picked box=%s", user.Username, boxname)
// After
slog.Info("box picked", "component", "session", "user", user.Username, "box", boxname)

// Before
log.Printf("[session] user not found for fp=%s", fp[:20])
// After
slog.Warn("session user not found", "component", "session", "fp", fp[:20])

// Before
log.Printf("[session] connect user=%s box=%s", user.Username, boxname)
// After
slog.Info("session connect", "component", "session", "user", user.Username, "box", boxname)

// Before
log.Printf("[session] resolve profile failed: %v", err)
// After
slog.Error("resolve profile failed", "component", "session", "err", err)

// Before
log.Printf("[session] setup cancelled: %v", err)
// After
slog.Warn("setup cancelled", "component", "session", "err", err)

// Before
log.Printf("[session] save user failed: %v", err)
// After
slog.Error("save user failed", "component", "session", "err", err)

// Before
log.Printf("[session] registered user=%s fp=%s", user.Username, fp[:20])
// After
slog.Info("user registered", "component", "session", "user", user.Username, "fp", fp[:20])

// Before
log.Printf("[session] save user profile failed: %v", err)
// After
slog.Error("save user profile failed", "component", "session", "err", err)

// Before (both occurrences)
log.Printf("[session] create box dir failed: %v", err)
// After
slog.Error("create box dir failed", "component", "session", "err", err)

// Before (both occurrences)
log.Printf("[session] save box profile failed: %v", err)
// After
slog.Error("save box profile failed", "component", "session", "err", err)

// Before
log.Printf("[session] build image failed: %v", err)
// After
slog.Error("build image failed", "component", "session", "err", err)

// Before
log.Printf("[session] create home dir failed: %v", err)
// After
slog.Error("create home dir failed", "component", "session", "err", err)

// Before
log.Printf("[session] container failed user=%s box=%s: %v", user.Username, boxname, err)
// After
slog.Error("container failed", "component", "session", "user", user.Username, "box", boxname, "err", err)

// Before
log.Printf("[session] attached user=%s box=%s container=%s", user.Username, boxname, containerID[:12])
// After
slog.Info("session attached", "component", "session", "user", user.Username, "box", boxname, "container", containerID[:12])

// Before
log.Printf("[session] no PTY user=%s", user.Username)
// After
slog.Warn("session no PTY", "component", "session", "user", user.Username)

// Before
log.Printf("[session] exec error user=%s: %v", user.Username, err)
// After
slog.Error("exec error", "component", "session", "user", user.Username, "err", err)

// Before
log.Printf("[session] disconnect user=%s box=%s", user.Username, boxname)
// After
slog.Info("session disconnect", "component", "session", "user", user.Username, "box", boxname)

// Before
log.Printf("[session] link code validation failed: %v", err)
// After
slog.Warn("link code validation failed", "component", "session", "err", err)

// Before
log.Printf("[session] link key failed: %v", err)
// After
slog.Error("link key failed", "component", "session", "err", err)

// Before
log.Printf("[session] linked fp=%s to user=%s", fp[:20], user.Username)
// After
slog.Info("key linked", "component", "session", "fp", fp[:20], "user", user.Username)

// Before
log.Printf("[session] picker cancelled for user=%s: %v", user.Username, err)
// After (second occurrence — identical)
slog.Warn("picker cancelled", "component", "session", "user", user.Username, "err", err)

// Before
log.Printf("WARNING: No host key configured, auto-generating to %s", keyPath)
// After
slog.Warn("no host key configured, auto-generating", "path", keyPath)
```

Swap `"log"` for `"log/slog"` in the imports.

### `internal/gateway/tunnel.go`

```go
// Before
log.Printf("[tunnel] resolve container failed: %v", err)
// After
slog.Error("tunnel resolve container failed", "component", "tunnel", "err", err)

// Before
log.Printf("[tunnel] %s:%d → %s (container %s)", d.DestAddr, d.DestPort, addr, containerID[:12])
// After
slog.Info("tunnel forward",
	"component", "tunnel",
	"dest_addr", d.DestAddr,
	"dest_port", d.DestPort,
	"target", addr,
	"container", containerID[:12])

// Before
log.Printf("[tunnel] dial failed %s: %v", addr, err)
// After
slog.Error("tunnel dial failed", "component", "tunnel", "target", addr, "err", err)
```

Swap `"log"` for `"log/slog"`.

### `internal/containers/manager.go`

```go
// Before
log.Printf("[idle] cancelled idle timer for container %s (session reconnected)", containerID[:12])
// After
slog.Info("idle timer cancelled (session reconnected)", "component", "idle", "container", containerID[:12])

// Before
log.Printf("[idle] starting %v idle timer for container %s", m.idleTimeout, containerID[:12])
// After
slog.Info("idle timer started", "component", "idle", "timeout", m.idleTimeout, "container", containerID[:12])

// Before
log.Printf("[idle] stopping idle container %s", containerID[:12])
// After
slog.Info("stopping idle container", "component", "idle", "container", containerID[:12])

// Before
log.Printf("[idle] failed to stop container %s: %v", containerID[:12], err)
// After
slog.Error("failed to stop idle container", "component", "idle", "container", containerID[:12], "err", err)

// Before
log.Printf("[container] profile hash mismatch for %s — removing and recreating", name)
// After
slog.Info("profile hash mismatch, recreating container", "component", "container", "name", name)

// Before
log.Printf("[container] creating %s with bind mount %s -> /home/dev", name, homePath)
// After
slog.Info("creating container", "component", "container", "name", name, "home", homePath)

// Before
log.Printf("[container] failed to create control socket: %v", err)
// After
slog.Error("failed to create control socket", "component", "container", "err", err)

// Before
log.Printf("[shutdown] closing socket server for container %s", id[:12])
// After
slog.Info("closing socket server", "component", "shutdown", "container", id[:12])

// Before
log.Printf("[shutdown] cancelled idle timer for container %s", id[:12])
// After
slog.Info("cancelled idle timer", "component", "shutdown", "container", id[:12])
```

Swap `"log"` for `"log/slog"`.

### `internal/containers/image.go`

Replace each `log.Printf` with `slog.Info`/`slog.Error` following the same pattern (`component: "image"`). Swap the import.

### `internal/containers/builder.go`

```go
// Before
log.Printf("[builder] building image %s for user %s", tag, username)
// After
slog.Info("building user image", "component", "builder", "tag", tag, "user", username)
```

Swap the import. (Additional build-duration timing is added in Task 8.)

### `internal/control/socket.go`

```go
// Before
log.Printf("[control] accept error on %s: %v", s.path, err)
// After
slog.Error("control socket accept error", "component", "control", "path", s.path, "err", err)
```

### `internal/control/handler.go`

```go
// Before
log.Printf("[control] destroying box %s (container %s)", info.BoxName, info.ContainerID[:12])
// After
slog.Info("destroying box", "component", "control", "box", info.BoxName, "container", info.ContainerID[:12])
```

### `internal/admin/server.go`

```go
// Before
log.Printf("[admin] template error: %v", err)
// After
slog.Error("template error", "component", "admin", "err", err)

// Before
log.Printf("[admin] failed to delete user %s: %v", username, err)
// After
slog.Error("failed to delete user", "component", "admin", "user", username, "err", err)

// Before
log.Printf("[admin] deleted user %s (fp=%s)", username, fp[:12])
// After
slog.Info("deleted user", "component", "admin", "user", username, "fp", fp[:12])

// Before
log.Printf("[admin] failed to delete box %s/%s: %v", username, boxname, err)
// After
slog.Error("failed to delete box", "component", "admin", "user", username, "box", boxname, "err", err)

// Before
log.Printf("[admin] deleted box %s/%s", username, boxname)
// After
slog.Info("deleted box", "component", "admin", "user", username, "box", boxname)

// Before
log.Printf("[admin] failed to stop container %s: %v", containerName, err)
// After
slog.Error("failed to stop container", "component", "admin", "container", containerName, "err", err)

// Before
log.Printf("[admin] stopped container %s", containerName)
// After
slog.Info("stopped container", "component", "admin", "container", containerName)

// Before
log.Printf("[admin] registration toggled to %v (runtime only)", enabled)
// After
slog.Info("registration toggled (runtime only)", "component", "admin", "enabled", enabled)
```

Swap `"log"` for `"log/slog"` in every touched file. Run `go build ./...` after this task to catch any accidentally missed import swaps.

---

## Task 4: Metrics package — definitions

Create new directory `internal/metrics/` and add `metrics.go`.

Add to `go.mod` (via `go get`):

```
go get github.com/prometheus/client_golang@latest
```

**File:** `internal/metrics/metrics.go`

```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Business metrics.
var (
	UsersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_users_total",
		Help: "Total number of registered users.",
	})

	BoxesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_boxes_total",
		Help: "Total number of boxes across all users.",
	})

	ContainersRunningTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_containers_running_total",
		Help: "Number of currently running hopbox containers.",
	})

	ActiveSessionsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hopbox_active_sessions_total",
		Help: "Current number of active SSH sessions.",
	})

	BoxConnectTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hopbox_box_connect_total",
		Help: "Total number of successful box connect events.",
	}, []string{"user", "box"})

	BuildDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hopbox_build_duration_seconds",
		Help:    "Duration of per-user image builds in seconds.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1s .. ~17min
	})
)

// Per-box resource metrics. Labels: user, box, container_id.
var boxLabels = []string{"user", "box", "container_id"}

var (
	BoxCPUPercent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_cpu_percent",
		Help: "CPU usage percentage for a box container.",
	}, boxLabels)

	BoxMemoryBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_memory_bytes",
		Help: "Current memory usage in bytes for a box container.",
	}, boxLabels)

	BoxMemoryLimitBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_memory_limit_bytes",
		Help: "Memory limit in bytes for a box container.",
	}, boxLabels)

	BoxNetworkRxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_network_rx_bytes",
		Help: "Total network bytes received by a box container.",
	}, boxLabels)

	BoxNetworkTxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_network_tx_bytes",
		Help: "Total network bytes transmitted by a box container.",
	}, boxLabels)

	BoxBlockReadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_block_read_bytes",
		Help: "Total block device bytes read by a box container.",
	}, boxLabels)

	BoxBlockWriteBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hopbox_box_block_write_bytes",
		Help: "Total block device bytes written by a box container.",
	}, boxLabels)
)

// DeleteBoxMetrics removes all per-box gauge series for the given labels.
// Called when a container disappears so metrics don't accumulate stale labels.
func DeleteBoxMetrics(user, box, containerID string) {
	BoxCPUPercent.DeleteLabelValues(user, box, containerID)
	BoxMemoryBytes.DeleteLabelValues(user, box, containerID)
	BoxMemoryLimitBytes.DeleteLabelValues(user, box, containerID)
	BoxNetworkRxBytes.DeleteLabelValues(user, box, containerID)
	BoxNetworkTxBytes.DeleteLabelValues(user, box, containerID)
	BoxBlockReadBytes.DeleteLabelValues(user, box, containerID)
	BoxBlockWriteBytes.DeleteLabelValues(user, box, containerID)
}
```

> NOTE: `rx`/`tx`/`block_read`/`block_write` are monotonic cumulative values from Docker stats. We expose them as `GaugeVec` (not `CounterVec`) because setting a counter's absolute value isn't supported. In Prometheus queries, operators should wrap them in `rate()` the same as counters.

---

## Task 5: Docker stats collector

**File:** `internal/metrics/collector.go`

```go
package metrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Collector polls Docker stats and updates per-box Prometheus metrics.
type Collector struct {
	cli      *client.Client
	interval time.Duration
	// key: containerID, value: (user, box) so we can delete stale metrics
	known map[string]boxKey
}

type boxKey struct {
	user string
	box  string
}

// NewCollector creates a stats collector that polls every 10 seconds.
func NewCollector(cli *client.Client) *Collector {
	return &Collector{
		cli:      cli,
		interval: 10 * time.Second,
		known:    make(map[string]boxKey),
	}
}

// Start runs the collector in a background loop until ctx is done.
func (c *Collector) Start(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()

	// Run once immediately so /metrics isn't empty at startup.
	c.collect(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	containers, err := c.cli.ContainerList(cctx, container.ListOptions{
		All:     false, // running only
		Filters: filters.NewArgs(filters.Arg("name", "hopbox-")),
	})
	if err != nil {
		slog.Warn("metrics collector: list containers failed", "err", err)
		return
	}

	seen := make(map[string]boxKey, len(containers))

	for _, ct := range containers {
		user, box, ok := parseContainerName(ct.Names)
		if !ok {
			continue
		}
		seen[ct.ID] = boxKey{user: user, box: box}

		if err := c.updateOne(cctx, ct.ID, user, box); err != nil {
			slog.Debug("metrics collector: stats failed", "container", ct.ID[:12], "err", err)
		}
	}

	// Delete metrics for containers that have gone away.
	for id, k := range c.known {
		if _, still := seen[id]; !still {
			DeleteBoxMetrics(k.user, k.box, id)
		}
	}
	c.known = seen
}

func parseContainerName(names []string) (user, box string, ok bool) {
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		if !strings.HasPrefix(n, "hopbox-") {
			continue
		}
		rest := strings.TrimPrefix(n, "hopbox-")
		// Format is hopbox-<user>-<box>; split on the LAST dash.
		i := strings.LastIndex(rest, "-")
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		return rest[:i], rest[i+1:], true
	}
	return "", "", false
}

func (c *Collector) updateOne(ctx context.Context, id, user, box string) error {
	resp, err := c.cli.ContainerStats(ctx, id, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var s dockerStats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}

	cpuPct := calcCPUPercent(&s)
	BoxCPUPercent.WithLabelValues(user, box, id).Set(cpuPct)
	BoxMemoryBytes.WithLabelValues(user, box, id).Set(float64(s.MemoryStats.Usage))
	BoxMemoryLimitBytes.WithLabelValues(user, box, id).Set(float64(s.MemoryStats.Limit))

	var rx, tx uint64
	for _, n := range s.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	BoxNetworkRxBytes.WithLabelValues(user, box, id).Set(float64(rx))
	BoxNetworkTxBytes.WithLabelValues(user, box, id).Set(float64(tx))

	var rd, wr uint64
	for _, e := range s.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			rd += e.Value
		case "write":
			wr += e.Value
		}
	}
	BoxBlockReadBytes.WithLabelValues(user, box, id).Set(float64(rd))
	BoxBlockWriteBytes.WithLabelValues(user, box, id).Set(float64(wr))
	return nil
}

// dockerStats is a minimal subset of the Docker stats JSON we need.
// We decode directly instead of importing container.StatsResponse so the
// collector stays decoupled from Docker SDK field renames.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64 `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

func calcCPUPercent(s *dockerStats) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta <= 0 || sysDelta <= 0 || cpus == 0 {
		return 0
	}
	return (cpuDelta / sysDelta) * cpus * 100.0
}
```

**Tests:** Add `internal/metrics/collector_test.go` with unit tests for:
- `parseContainerName` (happy path `hopbox-alice-dev` → `("alice","dev")`; rejects `hopbox-`, non-hopbox names, and malformed entries)
- `calcCPUPercent` with synthetic deltas (zero deltas → 0, nonzero → expected percentage)

---

## Task 6: Health check endpoint (+ restructure auth middleware)

**File:** `internal/admin/server.go`

Goal: `/healthz` is publicly accessible (no basic auth), while the existing pages and API stay behind auth. Split the mux so `basicAuth` wraps only the authenticated sub-mux.

Rewrite `NewAdminServer` mux wiring:

```go
func NewAdminServer(cfg *config.Config, store *users.Store, mgr *containers.Manager, dockerCli *client.Client) *AdminServer {
	s := &AdminServer{
		cfg:       cfg,
		store:     store,
		manager:   mgr,
		dockerCli: dockerCli,
	}

	// Authenticated routes live on their own sub-mux.
	authed := http.NewServeMux()
	authed.HandleFunc("GET /", s.handleDashboard)
	authed.HandleFunc("GET /users", s.handleUsers)
	authed.HandleFunc("GET /users/{username}/boxes", s.handleBoxes)
	authed.HandleFunc("GET /settings", s.handleSettings)
	authed.HandleFunc("DELETE /api/users/{username}", s.handleDeleteUser)
	authed.HandleFunc("DELETE /api/users/{username}/boxes/{boxname}", s.handleDeleteBox)
	authed.HandleFunc("POST /api/users/{username}/boxes/{boxname}/stop", s.handleStopBox)
	authed.HandleFunc("PUT /api/settings/registration", s.handleToggleRegistration)

	// Top-level mux: public routes direct, everything else via basicAuth(authed).
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.handleHealthz)
	root.Handle("GET /metrics", promhttp.Handler()) // see Task 7
	root.Handle("/", s.basicAuth(authed))

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Admin.Port),
		Handler: root,
	}

	return s
}
```

Add the health handler:

```go
func (s *AdminServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if _, err := s.dockerCli.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"unhealthy","docker":"unreachable","error":%q}`, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy","docker":"reachable"}`)
}
```

Add `"github.com/prometheus/client_golang/prometheus/promhttp"` to imports (used here and in Task 7).

**Tests:** Add `internal/admin/server_test.go`:
- Start an `AdminServer` with a fake/nil Docker client where possible, or use `httptest.NewServer` around the resulting handler; verify `/healthz` returns 200 (when Docker reachable) or 503 (on timeout/error). Use an interface or a small shim if `*client.Client` is hard to fake — otherwise cover `handleHealthz` via a direct call with a stubbed client.
- Verify `/metrics` returns 200 without auth.
- Verify `GET /` returns 401 without auth and 200 with valid basic auth.

---

## Task 7: Wire /metrics endpoint

Already wired above inside `NewAdminServer`:

```go
root.Handle("GET /metrics", promhttp.Handler())
```

No additional code — `promhttp.Handler()` automatically includes Go runtime metrics plus everything registered via `promauto` in `internal/metrics`. Verify the import path:

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"
```

Run `go mod tidy` after Tasks 4 and 7 to pull in Prometheus deps.

---

## Task 8: Wire metrics into manager, builder, and session handler

### `internal/containers/manager.go`

Increment/decrement gauges on lifecycle transitions. Import `"github.com/hopboxdev/hopbox/internal/metrics"`.

In `SessionConnect`, after incrementing `s.sessions`:

```go
metrics.ActiveSessionsTotal.Inc()
```

In `SessionDisconnect`, after decrementing `s.sessions` (only when it was actually decremented — i.e. when the container was tracked):

```go
// Replace the existing block:
s.sessions--
if s.sessions < 0 {
	s.sessions = 0
}
metrics.ActiveSessionsTotal.Dec()
```

Note: if `s.sessions` is already 0 before the decrement (the early `if !ok { return }` guards this), `Dec` still fires once per disconnect — safe because `SessionConnect` is always paired.

In `EnsureRunning`, increment `ContainersRunningTotal` only when a **new** container is successfully started (not when an existing one is found). Find the block that ends with `return resp.ID, nil` after `ContainerStart`, and add just before the return:

```go
metrics.ContainersRunningTotal.Inc()
return resp.ID, nil
```

Also, when we start an existing stopped container (inside the `if c.State != "running"` branch), increment it there as well:

```go
if c.State != "running" {
	if err := m.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}
	metrics.ContainersRunningTotal.Inc()
}
```

In `DestroyBox`, after successfully removing each container:

```go
if err := m.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
	return fmt.Errorf("remove container: %w", err)
}
metrics.ContainersRunningTotal.Dec()
```

Also decrement in `stopIdleContainer` on success:

```go
func (m *Manager) stopIdleContainer(containerID string) {
	slog.Info("stopping idle container", "component", "idle", "container", containerID[:12])
	ctx := context.Background()
	if err := m.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		slog.Error("failed to stop idle container", "component", "idle", "container", containerID[:12], "err", err)
	} else {
		metrics.ContainersRunningTotal.Dec()
	}
	m.mu.Lock()
	delete(m.states, containerID)
	m.mu.Unlock()
}
```

### `internal/containers/builder.go`

Time the build and observe the histogram. In `EnsureUserImage`, right before `slog.Info("building user image", ...)`:

```go
start := time.Now()
defer func() {
	metrics.BuildDurationSeconds.Observe(time.Since(start).Seconds())
}()
```

Only observe on actual builds — move the timer *after* the image-already-exists early return (which it already is, since we're inserting after that loop). Import `"time"` if not already imported and `"github.com/hopboxdev/hopbox/internal/metrics"`.

### `internal/gateway/server.go`

On successful connect in `sessionHandler`, after the `slog.Info("session attached", ...)` log line:

```go
metrics.BoxConnectTotal.WithLabelValues(user.Username, boxname).Inc()
```

Import `"github.com/hopboxdev/hopbox/internal/metrics"`.

### Users/boxes totals refresh

Add a small refresher in `cmd/hopboxd/main.go` that updates `UsersTotal` and `BoxesTotal` once at startup and every 30s. We do this in a goroutine to avoid plumbing the store into the metrics package:

```go
func startTotalsRefresher(ctx context.Context, store *users.Store) {
	refresh := func() {
		all := store.ListAll()
		metrics.UsersTotal.Set(float64(len(all)))
		boxes := 0
		for fp := range all {
			userDir := filepath.Join(store.Dir(), fp)
			names, _ := containers.ListBoxes(userDir)
			boxes += len(names)
		}
		metrics.BoxesTotal.Set(float64(boxes))
	}
	refresh()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh()
			}
		}
	}()
}
```

Call it from `main()` after creating the store.

---

## Task 9: Start collector in main.go

**File:** `cmd/hopboxd/main.go`

After creating the Docker client and running `cli.Ping`, create a root context, start the collector, and start the totals refresher. Pass the context to both so shutdown cancels the goroutines.

```go
rootCtx, rootCancel := context.WithCancel(context.Background())
defer rootCancel()

// ... existing store / manager setup ...

collector := metrics.NewCollector(cli)
go collector.Start(rootCtx)

startTotalsRefresher(rootCtx, store)
```

In the existing signal handler, call `rootCancel()` before `mgr.Shutdown()`:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
<-sigCh
slog.Info("shutting down")
rootCancel()
mgr.Shutdown()
srv.Close()
slog.Info("shutdown complete")
```

Import `"github.com/hopboxdev/hopbox/internal/metrics"`.

---

## Task 10: Grafana dashboards

Create `deploy/grafana/server-overview.json` and `deploy/grafana/box-details.json`. Both are valid Grafana 10+ JSON with a Prometheus datasource reference `${DS_PROMETHEUS}`.

### `deploy/grafana/server-overview.json`

```json
{
  "title": "Hopbox — Server Overview",
  "uid": "hopbox-server-overview",
  "schemaVersion": 38,
  "version": 1,
  "editable": true,
  "tags": ["hopbox"],
  "time": { "from": "now-1h", "to": "now" },
  "timepicker": {},
  "templating": { "list": [] },
  "refresh": "30s",
  "panels": [
    {
      "type": "stat",
      "title": "Registered users",
      "id": 1,
      "gridPos": { "h": 4, "w": 6, "x": 0, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "hopbox_users_total", "refId": "A" }
      ],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "stat",
      "title": "Total boxes",
      "id": 2,
      "gridPos": { "h": 4, "w": 6, "x": 6, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_boxes_total", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "stat",
      "title": "Running containers",
      "id": 3,
      "gridPos": { "h": 4, "w": 6, "x": 12, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_containers_running_total", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "stat",
      "title": "Active sessions",
      "id": 4,
      "gridPos": { "h": 4, "w": 6, "x": 18, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_active_sessions_total", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "timeseries",
      "title": "Active sessions over time",
      "id": 5,
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 4 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_active_sessions_total", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "legend": { "displayMode": "list" } }
    },
    {
      "type": "timeseries",
      "title": "User image build duration (p50/p95/p99)",
      "id": 6,
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 4 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "histogram_quantile(0.5, sum(rate(hopbox_build_duration_seconds_bucket[5m])) by (le))", "refId": "A", "legendFormat": "p50" },
        { "expr": "histogram_quantile(0.95, sum(rate(hopbox_build_duration_seconds_bucket[5m])) by (le))", "refId": "B", "legendFormat": "p95" },
        { "expr": "histogram_quantile(0.99, sum(rate(hopbox_build_duration_seconds_bucket[5m])) by (le))", "refId": "C", "legendFormat": "p99" }
      ],
      "fieldConfig": { "defaults": { "unit": "s" }, "overrides": [] },
      "options": { "legend": { "displayMode": "list" } }
    },
    {
      "type": "table",
      "title": "Running containers",
      "id": 7,
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 12 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "hopbox_box_cpu_percent", "refId": "A", "format": "table", "instant": true }
      ],
      "transformations": [
        { "id": "organize", "options": { "excludeByName": { "Time": true, "__name__": true, "job": true, "instance": true } } }
      ]
    },
    {
      "type": "timeseries",
      "title": "Goroutines",
      "id": 8,
      "gridPos": { "h": 8, "w": 6, "x": 12, "y": 12 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "go_goroutines", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] }
    },
    {
      "type": "timeseries",
      "title": "Go heap in use",
      "id": 9,
      "gridPos": { "h": 8, "w": 6, "x": 18, "y": 12 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "go_memstats_heap_inuse_bytes", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "bytes" }, "overrides": [] }
    }
  ]
}
```

### `deploy/grafana/box-details.json`

```json
{
  "title": "Hopbox — Box Details",
  "uid": "hopbox-box-details",
  "schemaVersion": 38,
  "version": 1,
  "editable": true,
  "tags": ["hopbox"],
  "time": { "from": "now-1h", "to": "now" },
  "timepicker": {},
  "refresh": "30s",
  "templating": {
    "list": [
      {
        "name": "user",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
        "query": "label_values(hopbox_box_cpu_percent, user)",
        "refresh": 2,
        "sort": 1
      },
      {
        "name": "box",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
        "query": "label_values(hopbox_box_cpu_percent{user=\"$user\"}, box)",
        "refresh": 2,
        "sort": 1
      }
    ]
  },
  "panels": [
    {
      "type": "stat",
      "title": "CPU %",
      "id": 1,
      "gridPos": { "h": 4, "w": 6, "x": 0, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_box_cpu_percent{user=\"$user\",box=\"$box\"}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "percent" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "stat",
      "title": "Memory in use",
      "id": 2,
      "gridPos": { "h": 4, "w": 6, "x": 6, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_box_memory_bytes{user=\"$user\",box=\"$box\"}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "bytes" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "stat",
      "title": "Memory limit",
      "id": 3,
      "gridPos": { "h": 4, "w": 6, "x": 12, "y": 0 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_box_memory_limit_bytes{user=\"$user\",box=\"$box\"}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "bytes" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"] }, "colorMode": "value" }
    },
    {
      "type": "timeseries",
      "title": "CPU usage",
      "id": 4,
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 4 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [{ "expr": "hopbox_box_cpu_percent{user=\"$user\",box=\"$box\"}", "refId": "A" }],
      "fieldConfig": { "defaults": { "unit": "percent" }, "overrides": [] }
    },
    {
      "type": "timeseries",
      "title": "Memory (usage vs limit)",
      "id": 5,
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 4 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "hopbox_box_memory_bytes{user=\"$user\",box=\"$box\"}", "refId": "A", "legendFormat": "usage" },
        { "expr": "hopbox_box_memory_limit_bytes{user=\"$user\",box=\"$box\"}", "refId": "B", "legendFormat": "limit" }
      ],
      "fieldConfig": { "defaults": { "unit": "bytes" }, "overrides": [] }
    },
    {
      "type": "timeseries",
      "title": "Network I/O (rate)",
      "id": 6,
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 12 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "rate(hopbox_box_network_rx_bytes{user=\"$user\",box=\"$box\"}[1m])", "refId": "A", "legendFormat": "rx" },
        { "expr": "rate(hopbox_box_network_tx_bytes{user=\"$user\",box=\"$box\"}[1m])", "refId": "B", "legendFormat": "tx" }
      ],
      "fieldConfig": { "defaults": { "unit": "Bps" }, "overrides": [] }
    },
    {
      "type": "timeseries",
      "title": "Disk I/O (rate)",
      "id": 7,
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 12 },
      "datasource": { "type": "prometheus", "uid": "${DS_PROMETHEUS}" },
      "targets": [
        { "expr": "rate(hopbox_box_block_read_bytes{user=\"$user\",box=\"$box\"}[1m])", "refId": "A", "legendFormat": "read" },
        { "expr": "rate(hopbox_box_block_write_bytes{user=\"$user\",box=\"$box\"}[1m])", "refId": "B", "legendFormat": "write" }
      ],
      "fieldConfig": { "defaults": { "unit": "Bps" }, "overrides": [] }
    }
  ],
  "__inputs": [
    {
      "name": "DS_PROMETHEUS",
      "label": "Prometheus",
      "type": "datasource",
      "pluginId": "prometheus",
      "pluginName": "Prometheus"
    }
  ]
}
```

---

## Task 11: Monitoring stack deployment

Create four files.

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
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
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

### `deploy/monitoring/README.md`

```markdown
# Hopbox Monitoring Stack

Prometheus + Grafana for scraping a host-running `hopboxd`.

## Prerequisites

1. Enable the admin server in `config.toml`:
   ```toml
   [admin]
   enabled  = true
   port     = 8080
   username = "admin"
   password = "change-me"
   ```
2. Restart `hopboxd`.

## Bring the stack up

```sh
cd deploy/monitoring
docker compose up -d
```

- Prometheus: http://localhost:9090
- Grafana:    http://localhost:3000  (admin / admin)

Prometheus scrapes `host.docker.internal:8080/metrics`, which reaches the
host-running hopboxd via the `host-gateway` extra host.

## Add the Prometheus datasource in Grafana

1. Log in.
2. Connections → Data sources → Add → Prometheus.
3. URL: `http://prometheus:9090`
4. Save & test.

## Import dashboards

See `../grafana/README.md`.
```

### `deploy/grafana/README.md`

```markdown
# Hopbox Grafana Dashboards

Two dashboards ship with hopbox:

- `server-overview.json` — users/boxes/containers/sessions, active sessions over time, build-duration quantiles, Go runtime.
- `box-details.json`    — per-box CPU / memory / network / disk. Uses `$user` and `$box` template variables.

## Import

1. Grafana → Dashboards → New → Import.
2. Upload JSON file.
3. Select the `Prometheus` datasource added via the monitoring stack.
4. Save.

Repeat for each dashboard.
```

---

## Task 12: Smoke test

Manual verification after all tasks are implemented:

1. **Build:** `go build ./...` — no errors.
2. **Vet/tests:** `go vet ./...` and `go test ./...` — all green.
3. **Run hopboxd** with `log_format = "json"` and `log_level = "debug"`; confirm JSON logs appear on stderr.
4. **Health:** `curl -sS -o - -w '\n%{http_code}\n' http://localhost:8080/healthz` → 200 with healthy JSON body when Docker is up.
5. **Metrics:** `curl -sS http://localhost:8080/metrics | grep hopbox_` shows business metrics immediately and per-box metrics within ~10 seconds after the first box is running.
6. **Auth preserved:** `curl -i http://localhost:8080/` returns 401; with `-u admin:PASS` returns 200.
7. **Lifecycle:**
   - `ssh` into a box → `hopbox_active_sessions_total` goes 0 → 1, `hopbox_containers_running_total` increments.
   - Disconnect → sessions counter decrements.
   - `hopbox_box_connect_total{user,box}` increments by 1.
   - Destroy the box from admin UI → `hopbox_containers_running_total` decrements, per-box labels disappear from `/metrics` within one collector tick.
8. **Builds:** Trigger a rebuild (change profile) → `hopbox_build_duration_seconds` histogram gains a new observation.
9. **Stack:** `cd deploy/monitoring && docker compose up -d`; Prometheus target at http://localhost:9090/targets shows `hopbox` as UP; import both dashboards and confirm panels render with real data.
10. **Shutdown:** `SIGINT` hopboxd; collector and totals refresher goroutines exit cleanly via `rootCtx` cancellation.

Record any gaps found and fix before closing Phase 5C.
