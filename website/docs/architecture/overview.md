---
sidebar_position: 1
---

# Architecture Overview

Hopbox is built as three Go binaries in a single monorepo.

## The three binaries

```
cmd/hop/        — Client CLI (macOS, Linux, Windows WSL)
cmd/hop-agent/  — Server daemon (Linux VPS, runs as systemd service)
cmd/hop-helper/ — Privileged helper (macOS LaunchDaemon / Linux systemd)
```

**hop** is the CLI you run on your developer machine. It manages the WireGuard tunnel, syncs workspace manifests, and runs bridges.

**hop-agent** runs on your VPS as a systemd service. It manages packages, services, snapshots, and exposes a control API over the WireGuard tunnel.

**hop-helper** is a privileged daemon that handles operations requiring root: creating TUN devices, configuring IP addresses and routes, and managing `/etc/hosts` entries.

## Communication model

```
┌──────────────────┐              ┌─────────────────────────┐
│   hop (client)   │  WireGuard   │    hop-agent (server)   │
│                  │   tunnel     │                         │
│  Tunnel setup    │◄────────────►│  Workspace lifecycle    │
│  Local bridges   │  10.10.0.1   │  Package installation   │
│  Health TUI      │  ◄────────►  │  Service orchestration  │
│  Snapshot cmds   │  10.10.0.2   │  Snapshot/restore       │
│                  │              │  Control API (:4200)    │
│  Runs on: laptop │  SSH (boot-  │                         │
│                  │  strap only) │  Runs on: Linux VPS     │
└──────────────────┘              └─────────────────────────┘

  hop-helper (privileged daemon)
    TUN device creation
    IP/route configuration
    /etc/hosts management
```

### WireGuard tunnel (primary transport)

All day-to-day communication flows over a WireGuard L3 tunnel. The tunnel creates a private network between your laptop (`10.10.0.1`) and VPS (`10.10.0.2`) over UDP port 51820.

Every TCP/UDP port on the server is directly reachable through the tunnel — no per-port SSH forwarding.

### SSH (bootstrap only)

SSH is used only during `hop setup` to install the agent and exchange WireGuard keys. After setup, SSH is not used for normal operations.

### Agent API

The agent control API listens at `http://<name>.hop:4200` over the WireGuard tunnel. It is never exposed to the public internet.

The API uses HTTP with a JSON-RPC dispatcher on `POST /rpc` and a health endpoint on `GET /health`. See the [agent API reference](../reference/agent-api.md) for details.

## Point-to-point topology

Hopbox uses a direct client-to-server connection. There is:

- **No coordination server** — keys are exchanged once over SSH
- **No DERP relay** — the VPS has a public IP, so NAT traversal is unnecessary
- **No account or cloud dependency** — everything runs on infrastructure you control

## Hostname convention

During `hop setup`, the helper daemon adds a `<name>.hop` entry to `/etc/hosts`. This makes the agent reachable as `mybox.hop:4200` from any process on your machine — no custom DNS or proxy required.

## Config files

| File | Purpose |
|------|---------|
| `~/.config/hopbox/hosts/<name>.yaml` | Per-host config (WireGuard keys, tunnel IPs, SSH endpoint) |
| `~/.config/hopbox/config.yaml` | Global settings (`default_host`) |
| `./hopbox.yaml` | Workspace manifest (packages, services, bridges) |
| `.env`, `.env.local` | Environment variables (loaded alongside manifest) |
