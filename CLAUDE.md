# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

Phase 0 implemented (Milestones 0a–0c). WireGuard tunnel, agent control API,
service management, bridges, and CLI commands are all in place.

## Build & Development Commands

```bash
# Build both binaries
go build ./cmd/hop/...
go build ./cmd/hop-agent/...

# Cross-compile agent for Linux (required for deployment)
CGO_ENABLED=0 GOOS=linux go build ./cmd/hop-agent/...

# Run tests
go test ./...

# Run a single test
go test ./internal/tunnel/... -run TestLoopbackWireGuard

# Lint (golangci-lint)
golangci-lint run

# Pre-commit hooks (prek)
prek install        # install git hooks once
prek run --all-files  # run all hooks manually

# Cross-compile releases
goreleaser build --snapshot
```

The agent binary is Linux-only; build with `GOOS=linux go build ./cmd/hop-agent/...`.

## Architecture

Two Go binaries in one monorepo:

```
cmd/hop/        — Client CLI (macOS/Linux/Windows WSL)
cmd/hop-agent/  — Server daemon (Linux VPS, runs as systemd service)
```

**Communication:** Wireguard L3 tunnel (UDP) is the primary transport. SSH is used only for initial bootstrap (`hop setup`) and as an emergency fallback. The agent's control API listens on `10.hop.0.2:4200` (Wireguard IP) — never exposed to the public internet.

**Client Wireguard mode:** Netstack (userspace via `wireguard-go/tun/netstack` + gVisor tcpip) — no root required on the developer's laptop.

**Server Wireguard mode:** Kernel TUN (preferred, requires CAP_NET_ADMIN); netstack fallback if unavailable.

**No coordination server, no DERP relay.** The server is a public-IP VPS. Key exchange happens once over SSH during `hop setup`; all subsequent communication is over Wireguard.

## Key Library Choices

| Component | Library |
|-----------|---------|
| Wireguard protocol | `git.zx2c4.com/wireguard-go` |
| Wireguard config | `github.com/WireGuard/wgctrl-go` |
| Userspace networking | `gvisor.dev/gvisor/pkg/tcpip` (via wireguard-go netstack) |
| Key generation | `golang.org/x/crypto/curve25519` |
| SSH (bootstrap only) | `golang.org/x/crypto/ssh` |

Do **not** use Tailscale's magicsock/DERP, tsnet, or libp2p — these are explicitly excluded as unnecessary for point-to-point client↔VPS topology.

## Workspace Manifest (hopbox.yaml)

The user-facing config file placed in a project directory. Declares everything for a workspace: `packages` (backend: nix/apt/static), `services` (type: docker/kubernetes/native), `bridges` (clipboard, chrome CDP, xdg-open), `env`, `secrets`, `scripts`, `backup`, `editor`, `session`.

Host connection config is stored at `~/.config/hopbox/hosts/<name>.yaml` (Wireguard keys, tunnel IPs, endpoint).

## Bridge System

Bridges fall into two categories:
1. **Just Wireguard routing** — any TCP/UDP service port is directly reachable at `10.hop.0.2:<port>`. No bridge code needed.
2. **True bridges** — resources that are inherently local: Chrome CDP (client→server direction), clipboard (bidirectional), xdg-open (server→client), notifications.

The bridge system implements only category 2.

## CLI Commands

```
hop setup <name> --host <ip>    Bootstrap: install agent, exchange WG keys, verify tunnel
hop up [workspace]              Bring up Wireguard tunnel + bridges + services
hop down                        Tear down tunnel and bridges
hop status                      Show tunnel, services, bridges health (TUI dashboard)
hop shell                       Drop into remote shell (zellij/tmux session)
hop run <script>                Execute named script from hopbox.yaml
hop services [ls|restart|stop]  Manage workspace services
hop logs [service]              Stream service logs
hop snap                        Snapshot workspace state to backup target (restic+S3)
hop snap restore <id>           Restore from snapshot
hop to <newhost>                Migrate workspace to new host (snap → setup → restore)
hop bridge [ls|restart]         Manage local-remote bridges
hop host [add|rm|ls]            Manage host registry
hop init                        Generate hopbox.yaml scaffold
```

## Agent Control API

HTTP/JSON-RPC on `10.hop.0.2:4200`. Port discovery uses `/proc/net/tcp` polling. Only reachable over the Wireguard tunnel.

## Coding Conventions

- Error variables must always be named `err`. Never use suffixed names like `werr`, `rerr`, `cerr`, etc. Use shadowing or restructure to avoid conflicts.

## Technical Decisions

- **Language:** Go — single binary, no runtime deps, same ecosystem as Coder/DevPod/Devbox
- **Config format:** YAML
- **Session manager:** zellij preferred, tmux supported
- **License:** Apache 2.0
- **Release tooling:** goreleaser
- **Windows host:** not supported (`hop-agent` is Linux-only; `hop` client supports Windows WSL)
