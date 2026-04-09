# Hopbox Production Readiness Design

## Overview

Production readiness pass covering graceful shutdown, systemd deployment, and user-facing documentation.

**Goal:** Make hopboxd deployable on a Linux server as a systemd service with clean shutdown behavior and a README that covers installation through first connection.

## Graceful Shutdown

**Current behavior:** SIGTERM calls `srv.Close()` which closes the SSH listener. Existing connections are dropped. Containers keep running.

**Changes:**
- Add `Manager.Shutdown()` that closes all active socket servers and cancels all idle timers
- Update `main.go` signal handler to call `Manager.Shutdown()` before `srv.Close()`
- Log shutdown progress

**Shutdown sequence:**
1. SIGTERM received
2. Log "shutting down..."
3. `Manager.Shutdown()` — close socket servers, cancel idle timers
4. `srv.Close()` — stop SSH listener, drop active connections
5. `cli.Close()` — close Docker client
6. Exit

Active SSH sessions are disconnected immediately. Containers keep running — users reconnect after restart.

## Systemd Unit File

`deploy/hopboxd.service`:

```ini
[Unit]
Description=Hopbox SSH Dev Environment Gateway
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=hopbox
Group=hopbox
WorkingDirectory=/opt/hopbox
ExecStart=/usr/local/bin/hopboxd --config /etc/hopbox/config.toml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**Expected filesystem layout on the server:**
```
/usr/local/bin/hopboxd           # binary
/etc/hopbox/config.toml          # config
/opt/hopbox/templates/           # Dockerfile.base + hopbox CLI binary
/var/lib/hopbox/                 # data dir (users, host key)
```

## README

Sections:
1. **What is Hopbox** — one paragraph
2. **Requirements** — Linux, Docker, Go 1.24+
3. **Quick Start** — build, run, first connection
4. **Configuration** — reference to config.example.toml, key options explained
5. **Usage** — SSH connection, boxnames, picker, wizard
6. **Deployment** — systemd setup, firewall (port 2222), host key management
7. **Architecture** — brief overview of how it works

## File Changes

```
README.md                          # new
deploy/hopboxd.service             # new
internal/containers/manager.go     # add Shutdown() method
cmd/hopboxd/main.go               # call Shutdown() on SIGTERM
```

## What This Does NOT Include

- Structured JSON logging (future)
- HTTP health check endpoint (future)
- Install script (manual setup via README)
- Contributing guide (future CONTRIBUTING.md)
