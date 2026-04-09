# Hopbox Phase 4 Design — Idle Timeout & Resource Limits

## Overview

Phase 4 adds idle timeout (auto-stop containers with no active SSH sessions) and per-container resource limits (CPU, memory, PIDs). Both are configured server-wide in `config.toml`.

**Goal:** Containers auto-stop after a configurable idle period to save resources. Each container is capped on CPU, memory, and process count to ensure fair multi-user sharing.

## Idle Timeout

**Definition of idle:** No active SSH sessions connected to the container. Running processes inside the container don't affect idle detection.

**Mechanism:**
- Manager tracks active SSH session count per container ID
- Session handler calls `Manager.SessionConnect(containerID)` on connect and `Manager.SessionDisconnect(containerID)` on disconnect
- When session count drops to 0, a `time.AfterFunc` timer starts with the configured timeout duration
- If a new session connects before the timer fires, the timer is cancelled
- When the timer fires, the container is stopped (`docker stop`) — not removed
- On next connect, `EnsureRunning` starts the stopped container as it already does

**Stopped vs removed:** Only stopped. Home directory, profile, and container state are preserved. Zellij session inside will be lost (process killed on stop), but the user's files persist.

**Config:**
```toml
idle_timeout_hours = 24  # 0 = disabled (containers run forever)
```

Default: 24 hours.

## Resource Limits

**Applied at container creation** via Docker's `HostConfig.Resources`. Only affects newly created containers — changing config doesn't affect running containers until they are recreated.

**Limits:**
- **CPU:** Number of CPU cores (converted to NanoCPUs for Docker API)
- **Memory:** GB of RAM (converted to bytes for Docker API)
- **PIDs:** Maximum number of processes (prevents fork bombs)

**Config:**
```toml
[resources]
cpu_cores = 2        # CPU cores per container (0 = unlimited)
memory_gb = 4        # GB RAM per container (0 = unlimited)
pids_limit = 512     # max processes per container (0 = unlimited)
```

Defaults: 2 cores, 4GB RAM, 512 PIDs.

**Docker HostConfig:**
```go
hostCfg := &container.HostConfig{
    Resources: container.Resources{
        NanoCPUs:  int64(cfg.Resources.CPUCores) * 1_000_000_000,
        Memory:    int64(cfg.Resources.MemoryGB) * 1024 * 1024 * 1024,
        PidsLimit: &cfg.Resources.PidsLimit,
    },
}
```

## Config Changes

```toml
port = 2222
data_dir = "./data"
host_key_path = ""
open_registration = true
idle_timeout_hours = 24

[resources]
cpu_cores = 2
memory_gb = 4
pids_limit = 512
```

## File Changes

**Modified files only — no new files:**
```
internal/config/config.go        # add IdleTimeoutHours, ResourcesConfig
internal/config/config_test.go   # test new config fields
internal/containers/manager.go   # session tracking, idle timer, resource limits
internal/gateway/server.go       # call SessionConnect/SessionDisconnect
```

## Manager Session Tracking

```go
type containerState struct {
    sessions   int
    idleTimer  *time.Timer
}

type Manager struct {
    cli        *client.Client
    sockets    map[string]*control.SocketServer
    states     map[string]*containerState  // containerID -> state
    mu         sync.Mutex
    idleTimeout time.Duration
}
```

**SessionConnect(containerID):** Increment session count. Cancel idle timer if running.

**SessionDisconnect(containerID):** Decrement session count. If 0, start idle timer.

**Idle timer fires:** `docker stop` the container. Log it. Clean up state.

## What Phase 4 Does NOT Include

- Per-user or per-box resource/timeout overrides (server-wide only)
- Admin commands
- Shared devboxes
- Disk quotas
