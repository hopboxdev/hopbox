# Hopbox Phase 3 Design — Picker TUI, Control Socket & In-Container CLI

## Overview

Phase 3 adds a box picker TUI for users with multiple boxes, a Unix socket-based control channel between containers and hopboxd, and a small `hopbox` CLI inside containers for status and destroy commands.

**Goal:** `ssh hop+?@server` shows a picker to select a box. Inside any box, `hopbox status` shows box info and `hopbox destroy` removes the current box.

## Picker TUI

**Trigger:** When the SSH username contains `+?` (e.g., `hop+?@server`), show the picker instead of connecting to a box.

**Behavior:**
- Parse `?` as a special boxname in `username.go`
- Query `data/users/<fp>/boxes/` for existing box directories
- Show a bubbletea Select list with box names and status (running/stopped)
- Select a box → connect to it (normal flow: resolve profile, ensure image, ensure container, exec)
- If no boxes exist → redirect to wizard (create `default` box)
- Esc/Ctrl+C → disconnect

**Picker display per box:**
```
myproject    zellij/bash    running
default      zellij/bash    stopped
api-server   tmux/zsh       running
```

**Implementation:** Reuse the same `runProgram()` helper from `wizard.go` for the single-`tea.Program` approach. The picker is a simple `huh.NewSelect` with box names as options.

## Control Socket

**Architecture:** hopboxd creates a Unix socket per container. The socket is bind-mounted into the container at `/var/run/hopbox.sock`. Each container gets its own socket so hopboxd can identify which container is making a request.

**Host-side socket path:** `/tmp/hopbox-<container-name>.sock`

**Container-side mount:** `/var/run/hopbox.sock`

**Protocol:** JSON-over-Unix-socket, one request/response per connection.

**Request format:**
```json
{"command": "status"}
{"command": "destroy", "confirm": "boxname"}
```

**Response format:**
```json
{"ok": true, "data": {"box": "default", "user": "gandalf", "os": "Ubuntu 24.04 (aarch64)", "shell": "bash", "multiplexer": "zellij", "uptime": "2h 34m"}}
{"ok": false, "error": "box name does not match"}
```

**Lifecycle:**
- Socket created on host before container starts
- Bind-mounted into container during `ContainerCreate`
- hopboxd starts a goroutine per socket listening for connections
- Socket and listener cleaned up when container stops or is destroyed

**Security:** The socket file is only accessible inside the container (mounted at a fixed path). Each container gets a unique socket so requests are scoped — hopboxd knows which container/box is asking by which socket received the request.

## In-Container CLI

A small static Go binary (`hopbox`) baked into the `hopbox-base` image at `/usr/local/bin/hopbox`. Connects to `/var/run/hopbox.sock`.

### `hopbox status`

Prints box info in human-readable format:

```
Box:         default
User:        gandalf
OS:          Ubuntu 24.04 (aarch64)
Shell:       bash
Multiplexer: zellij
Uptime:      2h 34m
```

With `--json` flag, outputs JSON:

```json
{"box":"default","user":"gandalf","os":"Ubuntu 24.04 (aarch64)","shell":"bash","multiplexer":"zellij","uptime":"2h 34m"}
```

### `hopbox destroy`

Confirmation prompt before destroying:

```
Are you sure you want to destroy box "default"? This will:
  - Stop and remove this container
  - Delete the home directory for this box

Type the box name to confirm: default
Destroying... done.
```

The destroy command:
1. Prompts for box name confirmation (must match current box)
2. Sends destroy request to hopboxd via socket
3. hopboxd stops and removes the container, deletes `boxes/<boxname>/` directory
4. Connection drops (container gone)

### Build

- Source: `cmd/hopbox/main.go` in the hopbox repo
- Cross-compiled for `linux/arm64` and `linux/amd64`
- A build script (`scripts/build-cli.sh`) compiles the correct binary based on host arch
- The binary is copied into the base image during `docker build` via a `COPY` instruction in `Dockerfile.base`
- The binary is a static build (`CGO_ENABLED=0`) so it works in any Linux container

## File Structure Changes

**New files:**
```
cmd/hopbox/main.go              # in-container CLI binary
internal/control/
├── socket.go                   # per-container socket server
├── handler.go                  # status + destroy command handlers
└── protocol.go                 # shared request/response types (used by CLI too)
internal/picker/
└── picker.go                   # bubbletea picker TUI
scripts/
└── build-cli.sh                # cross-compile hopbox CLI for linux
```

**Modified files:**
```
internal/gateway/server.go      # detect "?" boxname → run picker
internal/containers/manager.go  # mount socket, create/cleanup socket per container
templates/Dockerfile.base       # COPY hopbox CLI binary
```

## Container Lifecycle Changes

**Updated `EnsureRunning` flow:**
```
create socket on host → create container (with socket bind mount) → start container → start socket listener goroutine
```

**Updated destroy flow (via socket command):**
```
receive destroy request → stop container → remove container → delete box directory → cleanup socket → close listener
```

**Socket mount added to container config:**
```go
Binds: []string{
    fmt.Sprintf("%s:/home/dev", homePath),
    fmt.Sprintf("%s:/var/run/hopbox.sock", socketPath),
}
```

## What Phase 3 Does NOT Include

- `hopbox expose <port>` — port forwarding from inside container (future phase)
- `hopbox config` — re-running the wizard (users install tools manually)
- Admin commands (Phase 4)
- Idle timeout auto-stop (Phase 4)
