# Hopbox Roadmap

**One-liner:** A self-hostable workspace runtime that makes remote development feel local.

Hopbox is an open-source client-server tool that lets developers define, deploy,
connect to, and migrate complete development workspaces on any Linux host. It manages
the full workspace lifecycle — toolchains, services, Wireguard networking, local-remote
bridging, sessions, backups, and mobility — from a single declarative config (`hopbox.yaml`).

See also: [Product Overview](docs/product-overview.md) for positioning, target audience,
and competitive analysis.

---

## Current State

Phases 0 and 1 are complete. Phase 2 is next.

**What works today:** `hop setup`, `hop up`, `hop status`, `hop to`, `hop upgrade`,
`hop rotate`, `hop code`, `hop init`, `hop run`, `hop services`, `hop logs`,
`hop snap`, `hop bridge ls`, `hop host`. Kernel TUN on both client (macOS utun via
helper daemon) and server (Linux). Bubbletea TUI step runner for multi-phase commands.
Reconnection monitoring with 5-second heartbeat. Clipboard and Chrome CDP bridges.
Docker service orchestration with dependency ordering and health checks. Snapshot/restore
via restic. Workspace migration across hosts via `hop to`. `.env` / `.env.local` file
loading with workspace env merge into all services.

---

## Phase 0 — Dogfood & Skeleton (Done)

### Milestone 0a: Wireguard tunnel

- [x] Go module scaffolding — `cmd/hop/`, `cmd/hop-agent/`, `cmd/hop-helper/`
- [x] Wireguard key management — keypair generation, hex/base64 conversion, file storage
- [x] hop-agent Wireguard listener — kernel TUN on Linux, netstack fallback
- [x] hop client Wireguard — kernel TUN via helper (macOS utun), netstack for `hop to`
- [x] `hop setup` — SSH TOFU, agent install, key exchange, helper install prompt
- [x] `hop up` — kernel TUN tunnel, agent probe, manifest sync, bridges, ConnMonitor
- [x] `hop down` — foreground Ctrl-C teardown
- [x] `hop status` — lipgloss dashboard with tunnel state, ping, services, bridges

### Milestone 0b: Agent control + services

- [x] hop-agent control API — HTTP/JSON-RPC on `10.10.0.2:4200` over Wireguard only
- [x] hopbox.yaml parser — full manifest schema with validation
- [x] Package installation — apt and nix backends
- [x] Docker service management — start/stop/restart, health checks, dependency ordering
- [x] Port monitoring — `/proc/net/tcp` parsing with process name resolution
- [x] Automatic port forwarding — discover remote ports and proxy locally
- [x] `hop services ls/restart/stop`

### Milestone 0c: Bridges

- [x] Bridge: clipboard — TCP listener, pbcopy/xclip integration, bidirectional
- [x] Bridge: Chrome CDP — TCP proxy on port 9222
- [x] `hop run <script>` — execute scripts from hopbox.yaml

---

## Phase 1 — Core Feature Set (Done)

- [x] Service orchestration — dependency ordering, health check polling, log aggregation, data directory declarations
- [x] Snapshot & restore — `hop snap create/restore/ls`, restic backend, metadata tracking
- [x] `hop to <newhost>` — 3-phase migration: snapshot, bootstrap new host, restore
- [x] Reconnection resilience — 5s heartbeat, ConnMonitor, state file, auto-recovery reporting
- [x] Configuration management — host registry, global config, `hop init`
- [x] `hop upgrade` — GitHub release download, SHA256 verify, helper sudo install, `--local` dev mode
- [x] `hop rotate` — key rotation without full re-setup
- [x] `hop code` — VS Code remote SSH integration
- [x] `hop logs` — stream service logs (single + all)
- [x] Bubbletea TUI step runner — phased runner with spinners, used by setup/up/to/upgrade
- [x] CLI styling — lipgloss-based `internal/ui` package with Section, Step, Table, Row primitives
- [x] `hop up` as background daemon — refactor from foreground process to background service
- [x] `hop down` — proper teardown command that signals the background `hop up` process

---

## Phase 2 — Open Source Release (Planned)

- [ ] Linux client support — helper daemon or direct TUN setup for Linux laptops
- [ ] `hop to` error recovery — rollback or resume on mid-migration failure
- [x] CI/CD pipeline — GitHub Actions for tests, linting, cross-compilation, goreleaser for releases
- [ ] Package management abstraction — backend interface, lock file (`hopbox.lock`)
- [x] Static package backend — download binary from URL
- [x] Packages in `hop status` — show installed packages from manifest in dashboard
- [ ] Package reconciliation — remove stale binaries/packages not in current manifest
- [x] Native service backend — run processes directly without Docker
- [ ] devcontainer.json compatibility (read-only import)
- [ ] Network fallbacks for restricted environments — SSH tunneling (`hop up --ssh`), Wireguard-over-WebSocket
- [ ] Installation script (`curl | sh`) + Homebrew tap + AUR
- [ ] Documentation site (hopbox.dev) — quickstart, manifest reference, migration guides
- [ ] GitHub repo public release — Apache 2.0, example configs
- [x] xdg-open bridge — server `xdg-open` opens URLs in local browser
- [x] Notifications bridge — remote build notifications to local desktop
- [x] Secrets management — .env file loading, workspace env merge
- [ ] Test coverage — expand unit and integration tests across packages

---

## Phase 3 — Community & Ecosystem (Future)

- [ ] Plugin system for bridges — Bridge interface with Setup/Teardown/HealthCheck
- [ ] Multi-workspace management — `hop list`, `hop switch`
- [ ] Editor integrations — VS Code extension, Neovim plugin
- [ ] hop-hub (optional control plane) — lightweight web dashboard for small teams
- [ ] Session manager integration — zellij/tmux auto-attach on `hop up`
- [ ] Optional DERP relay for team scenarios behind NAT

---

## Phase 4 — Sustainability (Future)

- [ ] hop-hub Cloud — hosted control plane, free for 1 workspace
- [ ] Managed backup storage — encrypted snapshots, pay per GB
- [ ] Priority support for teams
- [ ] Workspace templates marketplace

---

## Architecture (Brief)

Three Go binaries:

```
cmd/hop/        — Client CLI (macOS/Linux/Windows WSL)
cmd/hop-agent/  — Server daemon (Linux VPS, runs as systemd service)
cmd/hop-helper/ — Privileged helper (macOS LaunchDaemon, TUN + /etc/hosts)
```

Communication: Wireguard L3 tunnel (UDP). SSH for bootstrap only. Agent API at
`<name>.hop:4200` over Wireguard — never exposed publicly.

Client uses kernel TUN (utun on macOS) via the helper daemon. Server uses kernel
TUN (Linux). Netstack is used only for `hop to` migration tunnels.

No coordination server, no DERP relay. Direct client-to-VPS topology.

---

## Non-Goals

- **Full Nix purity** — Nix is one package backend, not the foundation
- **Browser-based IDE** — we connect your local editor, not host code-server
- **AI agent hosting** — not building sandboxes
- **Windows host support** — hop-agent is Linux-only; client runs anywhere
- **GUI application** — CLI-first, `hop status` is the richest UI
- **Kubernetes management** — k3s/k8s services work over the tunnel without special support
- **Mesh networking** — point-to-point only (client-to-server)
