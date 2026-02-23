# Daemon Mode Design

Background daemon for `hop up` with `hop down` teardown and `hop daemon` subcommand.

## Problem

`hop up` runs as a foreground process — the tunnel dies when the terminal closes.
Users need a persistent tunnel that survives terminal sessions, with a clean way
to tear it down.

## Architecture

Three roles with clear boundaries:

| Component | Responsibility | Lifetime |
|-----------|---------------|----------|
| `hop daemon start <host>` | TUN device, WireGuard, bridges, ConnMonitor, Unix socket IPC | Long-lived background process |
| `hop up [workspace]` | TUI init (agent probe, packages, manifest sync), then launches daemon | Short-lived (exits after init) |
| `hop down` / `hop daemon stop` | Sends shutdown to daemon via socket | Instant |

### Data Flow

```
hop up                          hop daemon start <host>
  |                                     |
  +- Launch daemon (detached) --------> | create TUN via helper
  |                                     | start WireGuard
  |   poll socket for readiness <------ | start bridges
  |                                     | start ConnMonitor
  |   tunnel ready                      | listen on Unix socket
  |                                     | write state file
  +- TUI: probe agent                   | ... runs indefinitely ...
  +- TUI: sync manifest                 |
  +- TUI: install packages              |
  +- "Tunnel <host> running" -> exit    |
                                        |
hop down                                |
  |                                     |
  +- connect to socket --------------> | receive shutdown
  |   "ok" <--------------------------- | cleanup (bridges, TUN, hosts, state)
  +- exit                               +- exit
```

Key: TUI phases run _after_ the daemon starts, because they need the active
tunnel to reach the agent at `<host>.hop:4200`.

## Daemon Process (`hop daemon start`)

### Startup Sequence

1. Load host config from `~/.config/hopbox/hosts/<name>.yaml`
2. Create TUN device via helper (`helper.Client.CreateTUN`)
3. Start WireGuard on the TUN device
4. Wait for TUN ready (`tun.Ready()` channel)
5. Configure TUN IP + routes via helper (`ConfigureTUN`)
6. Add `/etc/hosts` entry via helper (`AddHost`)
7. Start bridges as goroutines (clipboard, CDP — from manifest if provided)
8. Start ConnMonitor goroutine
9. Write state file to `~/.config/hopbox/run/<host>.json`
10. Create Unix socket at `~/.config/hopbox/run/<host>.sock`
11. Accept connections (signals readiness to `hop up`)
12. Block until shutdown

### Shutdown Sequence

1. Receive shutdown command (via socket or SIGTERM/SIGINT)
2. Cancel context -> stops ConnMonitor, bridges
3. Cleanup TUN via helper (`CleanupTUN`)
4. Remove `/etc/hosts` entry (`RemoveHost`)
5. Remove state file
6. Close and remove socket file
7. Exit

### Signal Handling

- **SIGTERM:** Graceful shutdown (same as socket `shutdown` command)
- **SIGINT:** Graceful shutdown (for `--foreground` mode Ctrl-C)
- **SIGHUP:** Ignored (daemon survives terminal close)

### Code Location

New package `internal/daemon/`:
- `daemon.go` — `Run(ctx, hostConfig, manifest)` main loop
- `socket.go` — Unix socket server: listener, JSON request handler
- `client.go` — Unix socket client: connect, send request, read response

New command file: `cmd/hop/daemon.go` — Kong subcommand struct.

## Unix Socket IPC Protocol

### Socket Location

`~/.config/hopbox/run/<host>.sock` — alongside the existing state file.

### Protocol

One request-response per connection (connect, send, read, close). No persistent
connections.

**Request:**
```json
{"method": "status"}
{"method": "shutdown"}
```

**Response:**
```json
{"ok": true, "state": {"connected": true, "last_healthy": "...", "pid": 12345, "interface": "utun5", "started_at": "...", "bridges": ["clipboard", "cdp"]}}
{"ok": true}
{"ok": false, "error": "not running"}
```

### Methods

| Method | Purpose | Used by |
|--------|---------|---------|
| `status` | Returns live daemon state | `hop daemon status`, `hop status` |
| `shutdown` | Graceful shutdown, responds after cleanup | `hop daemon stop`, `hop down` |

### Readiness Detection

`hop up` polls the socket every 200ms after launching the daemon. Once the
socket accepts a connection, the tunnel is up. Timeout after 15 seconds.

## `hop up` Refactoring

### Default Mode (Daemon)

1. `resolveHost()` — same resolution order as today
2. Check for existing daemon: try connect to socket
   - Connected -> daemon running, skip to step 5
   - Not connected -> proceed to step 3
3. Launch daemon: `exec.Command("hop", "daemon", "start", hostName).Start()`
   - Detach from parent (`SysProcAttr` with `Setsid`)
   - Redirect stdout/stderr to `~/.config/hopbox/run/<host>.log`
4. Wait for readiness: poll socket 200ms intervals, 15s timeout
   - On timeout -> print error, kill daemon, exit
5. Run TUI phases (same as today):
   - Probe agent at `<host>.hop:4200`
   - Load `hopbox.yaml` manifest (optional)
   - Sync manifest to agent
   - Install packages
6. Print "Tunnel <host> running (PID <pid>)"
7. Exit

### `--foreground` Mode

Same as today's behavior — all-in-one blocking process. The daemon code runs
inline (no child process). Ctrl-C tears everything down. Useful for debugging.

### Re-attach Behavior

If `hop up` detects an already-running daemon (step 2), it skips launching and
goes straight to TUI phases. This lets users re-run `hop up` to re-sync
manifests or install new packages without restarting the tunnel.

## `hop down`

1. `resolveHost()` — same resolution order
2. Connect to `~/.config/hopbox/run/<host>.sock`
3. If socket doesn't exist -> "No tunnel running for <host>"
4. Send `{"method": "shutdown"}`
5. Wait for `{"ok": true}` (10s timeout)
6. Print "Tunnel <host> stopped."

`hop down` is a direct alias — it calls `daemon.Client.Shutdown()`, same as
`hop daemon stop`.

## `hop daemon` Subcommands

```go
type DaemonCmd struct {
    Start  DaemonStartCmd  `cmd:"" help:"Start tunnel daemon."`
    Stop   DaemonStopCmd   `cmd:"" help:"Stop tunnel daemon."`
    Status DaemonStatusCmd `cmd:"" help:"Show daemon status."`
}
```

- `hop daemon start <host>` — runs daemon in foreground (intended to be launched by `hop up` or directly by power users)
- `hop daemon stop <host>` — sends shutdown via socket
- `hop daemon status <host>` — queries live state via socket

## `hop status` Updates

Currently reads the state file. With the daemon, it can optionally query the
daemon socket for live data (connected status, bridges, last_healthy). Falls
back to state file if daemon isn't responding.

## Edge Cases

### Stale Daemon Detection

Daemon crashes without cleanup (SIGKILL, power loss):
- Socket file exists but nobody's listening
- State file exists with a dead PID

Resolution: `LoadState()` already checks PID liveness. Socket connection fails.
`hop up` detects both -> removes stale socket + state file -> launches fresh
daemon.

### Double `hop up`

`hop up` while daemon running: detects daemon via socket, skips launch, runs
TUI phases (re-sync). Intentional and useful.

### Double `hop daemon start`

Checks for socket. If connectable, prints "Daemon already running for <host>
(PID <pid>)" and exits 1.

### `hop up --foreground` with Running Daemon

Prints "Daemon already running for <host>. Use `hop down` first, or run
without --foreground." Exits 1.

### Daemon Crash Recovery

Helper-level resources (TUN, /etc/hosts, routes) are not cleaned up if daemon
is killed. On next `hop up`, helper creates a new utun (old one is orphaned
but harmless — macOS cleans up on reboot). State file and socket cleaned up by
stale detection.

### Daemon Log File

Stdout/stderr redirected to `~/.config/hopbox/run/<host>.log`. Truncated on
each daemon start. No rotation.

## Summary of Decisions

| Decision | Choice |
|----------|--------|
| Background mechanism | Separate `hop daemon` subcommand (user-facing) |
| IPC | Unix socket + JSON at `~/.config/hopbox/run/<host>.sock` |
| `hop down` | Alias for `hop daemon stop` |
| Foreground mode | `hop up --foreground` keeps old blocking behavior |
| Readiness | Socket polling (200ms interval, 15s timeout) |
| Daemon owns | TUN, WireGuard, bridges, ConnMonitor, state file, socket |
| `hop up` owns | TUI phases (agent probe, manifest sync, packages) |
| Re-attach | `hop up` with running daemon skips launch, re-runs TUI phases |
| New code | `internal/daemon/` package, `cmd/hop/daemon.go` command |
