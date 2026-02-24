# Native Service Backend Design

## Summary

Add a native service backend so processes can run directly on the host without Docker. A native service is declared with `type: native` and a `command` field in `hopbox.yaml`. The existing `Backend` interface already supports this; the work is implementing `NativeBackend` and wiring it into the agent.

## Manifest Schema

```yaml
services:
  my-api:
    type: native
    command: "./my-server --port 8080"
    workdir: /home/user/myproject
    ports: ["8080"]
    env:
      DATABASE_URL: "postgres://localhost:5432/mydb"
    health:
      http: "http://localhost:8080/health"
    depends_on: [postgres]
```

- `command` — required for native, run via `sh -c "<command>"`
- `workdir` — optional, defaults to agent working directory
- `image` ignored for native; `command` ignored for docker
- All other fields (`ports`, `env`, `health`, `depends_on`, `data`) work identically for both types

## Process Lifecycle

**Start:** `sh -c "<command>"` with `Cmd.Dir` set to `workdir`. Environment is agent env merged with service `env` map. Process gets its own process group (`Setpgid: true`) so stop can kill the whole tree.

**Stop:** `SIGTERM` to process group. Wait up to 5 seconds. `SIGKILL` if still alive.

**IsRunning:** `syscall.Kill(pid, 0)`.

**Auto-restart:** Supervisor goroutine watches the process. On non-zero exit, restart with exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s (capped). Backoff resets after 60s of healthy running. Explicit stop (via `hop services stop`) suppresses restart.

**Logging:** stdout/stderr append to `~/.config/hopbox/logs/<service>.log`. No rotation.

## Dependency Ordering

Same topological sort as Docker services. Dependencies work across types — a native service can depend on a Docker service and vice versa. On stop, reverse dependency order: stop dependents before their dependencies.

## Log Streaming

Add `LogCmd(name string, tail int) *exec.Cmd` to the `Backend` interface:
- Docker: `docker logs --follow --tail <n> -- <name>`
- Native: `tail -n <n> -f <logpath>`

`rpcLogsStream` dispatches to the backend's `LogCmd` instead of hardcoding Docker. Fan-out for "all services" mode works the same way.

## Changes

**New files:**
- `internal/service/native.go` — NativeBackend: Backend implementation, supervisor goroutine, process group management

**Modified files:**
- `internal/service/manager.go` — Add LogCmd to Backend interface, reverse-order StopAll, Backend(name) accessor
- `internal/service/docker.go` — Add LogCmd method
- `internal/manifest/manifest.go` — Add Workdir field, validate native requires command
- `internal/agent/services.go` — Wire NativeBackend in BuildServiceManager
- `internal/agent/api.go` — Refactor rpcLogsStream to use backend.LogCmd

**Tests:**
- NativeBackend unit tests (start, stop, is-running, restart-on-crash, backoff reset)
- Reverse dependency stop order
- Mixed native + Docker service startup
