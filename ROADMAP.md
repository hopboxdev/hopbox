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

Phase 0 is complete. Phase 1 is in progress.

**What works today:** `hop setup`, `hop up`, `hop status`, `hop to`, `hop upgrade`,
`hop rotate`, `hop code`, `hop init`, `hop run`, `hop services`, `hop logs`,
`hop snap`, `hop bridge ls`, `hop host`. Kernel TUN on both client (macOS utun via
helper daemon) and server (Linux). Bubbletea TUI step runner for multi-phase commands.
Reconnection monitoring with 5-second heartbeat. Clipboard and Chrome CDP bridges.
Docker service orchestration with dependency ordering and health checks. Snapshot/restore
via restic. Workspace migration across hosts via `hop to`.

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
- [x] Port monitoring — `/proc/net/tcp` parsing
- [x] `hop services ls/restart/stop`

### Milestone 0c: Bridges + shell

- [x] Bridge: clipboard — TCP listener, pbcopy/xclip integration, bidirectional
- [x] Bridge: Chrome CDP — TCP proxy on port 9222
- [x] `hop run <script>` — execute scripts from hopbox.yaml
- [ ] SSH fallback — `hop up --ssh` when Wireguard is blocked

---

## Phase 1 — Core Feature Set (In Progress)

- [x] Service orchestration — dependency ordering, health check polling, log aggregation, data directory declarations
- [ ] k3s as a first-class service type — install k3s, auto-apply manifests, kubectl just works
- [x] Snapshot & restore — `hop snap create/restore/ls`, restic backend, metadata tracking
- [x] `hop to <newhost>` — 3-phase migration: snapshot, bootstrap new host, restore
- [x] Reconnection resilience — 5s heartbeat, ConnMonitor, state file, auto-recovery reporting
- [ ] TUI status dashboard — live tunnel health, service status, bridge state, quick actions
- [x] Configuration management — host registry, global config, `hop init`
- [x] `hop upgrade` — GitHub release download, SHA256 verify, helper sudo install, `--local` dev mode
- [x] `hop rotate` — key rotation without full re-setup
- [x] `hop code` — VS Code remote SSH integration
- [x] `hop logs` — stream service logs (single + all)
- [x] Bubbletea TUI step runner — phased runner with spinners, used by setup/up/to/upgrade
- [x] CLI styling — lipgloss-based `internal/ui` package with Section, Step, Table, Row primitives

---

## Phase 2 — Open Source Release (Planned)

- [ ] Package management abstraction — backend interface, lock file (`hopbox.lock`)
- [ ] Static package backend — download binary from URL
- [ ] devcontainer.json compatibility (read-only import)
- [ ] Wireguard-over-WebSocket fallback for restricted networks
- [ ] Installation script (`curl | sh`) + Homebrew tap + AUR
- [ ] Documentation site (hopbox.dev) — quickstart, manifest reference, migration guides
- [ ] GitHub repo public release — Apache 2.0, CI, example configs
- [ ] Native service backend — run processes directly without Docker
- [ ] xdg-open bridge — server `xdg-open` opens URLs in local browser
- [ ] Notifications bridge — remote build notifications to local desktop
- [ ] Secrets management — sops/age integration, `hop secret set`

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
- **GUI application** — CLI-first, TUI dashboard is the richest UI
- **Kubernetes operator** — hop-agent is a daemon on bare Linux
- **Mesh networking** — point-to-point only (client-to-server)
