# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

Phase 0 implemented (Milestones 0a–0c). WireGuard tunnel, agent control API,
service management, bridges, and CLI commands are all in place.
`hop setup` + `hop up` verified working end-to-end.

## Build & Development Commands

```bash
# Build both binaries
go build ./cmd/hop/...
go build ./cmd/hop-agent/...

# Cross-compile agent for Linux (required for deployment)
CGO_ENABLED=0 GOOS=linux go build -o dist/hop-agent-linux ./cmd/hop-agent/...

# Dev workflow: use a local agent binary during hop setup
HOP_AGENT_BINARY=./dist/hop-agent-linux hop setup mybox -a <ip> -u debian -k ~/.ssh/key

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

The `host:` field in `hopbox.yaml` pins which registered host to use for this workspace.

Config files:
- `~/.config/hopbox/hosts/<name>.yaml` — per-host (WireGuard keys, tunnel IPs, SSH endpoint, SSH host key)
- `~/.config/hopbox/config.yaml` — global user settings (`default_host`)

## Bridge System

Bridges fall into two categories:
1. **Just Wireguard routing** — any TCP/UDP service port is directly reachable at `10.hop.0.2:<port>`. No bridge code needed.
2. **True bridges** — resources that are inherently local: Chrome CDP (client→server direction), clipboard (bidirectional), xdg-open (server→client), notifications.

The bridge system implements only category 2.

## CLI Commands

```
hop setup <name> -a <ip> [-u user] [-k keyfile] [-p port]
                                Bootstrap: install agent, exchange WG keys, save host config
hop up [workspace]              Bring up WireGuard tunnel + bridges + services
hop down                        Tear down tunnel (Ctrl-C in foreground mode)
hop status                      Show host config and agent health
hop shell                       Drop into remote shell (zellij/tmux session per hopbox.yaml)
hop run <script>                Execute named script from hopbox.yaml
hop services [ls|restart|stop]  Manage workspace services
hop logs [service]              Stream service logs
hop snap [create|restore|ls]    Manage workspace snapshots (restic backend)
hop to <newhost>                Migrate workspace to new host (snap → restore)
hop bridge [ls|restart]         Manage local-remote bridges
hop host [add|rm|ls|default]    Manage host registry; default shows/sets default host
hop init                        Generate hopbox.yaml scaffold
hop version                     Print version info
```

Host resolution order (all commands that need a host):
1. `--host`/`-H` flag
2. `host:` field in `./hopbox.yaml`
3. `default_host` in `~/.config/hopbox/config.yaml`
4. Error — user must specify one of the above

`hop setup` auto-sets the new host as default if no default is configured yet.

## Agent Control API

HTTP/JSON-RPC on `10.10.0.2:4200` (`tunnel.AgentAPIPort`). Only reachable over
the WireGuard tunnel — never exposed to the public internet.

Endpoints: `GET /health`, `POST /rpc`

RPC methods: `services.list`, `services.restart`, `services.stop`, `ports.list`,
`run.script`, `logs.stream`, `packages.install`, `snap.create`, `snap.restore`,
`snap.list`, `workspace.sync`

`ports.list` uses `/proc/net/tcp` on the server to discover listening ports.

`logs.stream` is the only method that does **not** return a JSON envelope — it
streams `text/plain` output directly (docker logs --follow). Use
`rpcclient.CopyTo` on the client side, not `rpcclient.Call`.

**RPC client (`internal/rpcclient`):** `Call`/`CallVia`/`CallAndPrint` read the
full JSON response. Use `CallVia` inside `UpCmd` (needs `tun.DialContext`); use
`Call` from other commands (uses tunnel state proxy address). Use `CopyTo` for
streaming responses.

**Important:** On macOS after `hop up`, `10.10.0.2` only exists inside the
`hop up` process (netstack). Commands outside that process reach the agent via
the localhost proxy written to tunnel state. `Call` handles this automatically.

## Coding Conventions

- Error variables must always be named `err`. Never use suffixed names like `werr`, `rerr`, `cerr`, etc. Use shadowing or restructure to avoid conflicts.
- Service definitions are `service.Def` (not `ServiceDef`).
- `cmd/hop/main.go` is intentionally thin — commands live in separate files (`up.go`, `setup.go`, `status.go`, etc.).

## Known Pitfalls

**Client netstack:** `10.10.0.2` only exists inside the process. All agent HTTP
calls from `cmd/hop` must use `tun.DialContext` as the transport — a plain
`http.Client` will fail (no OS route). Pattern:
```go
agentClient := &http.Client{Transport: &http.Transport{DialContext: tun.DialContext}}
```

**Agent bind race:** `net.Listen("tcp", "10.10.0.2:4200")` must not be called
before the WireGuard interface is up. `ServerTunnel.Ready()` closes a channel
once the interface is assigned; `RunOnAddr` waits on it before binding.

**SSH stdout after large upload:** `runRemote` stdout is unreliable immediately
after uploading a large binary (returns null bytes). Read results from files
the command writes instead (e.g. `sudo grep '^public=' /etc/hopbox/agent.key`).

**TOFU on `hop setup`:** The first SSH connection prompts the user to confirm
the host key fingerprint (yes/no). Pass `setup.Options.ConfirmReader` in tests
(e.g. `strings.NewReader("yes\n")`) to avoid blocking on stdin.

**Replacing a running binary:** Overwriting an executing binary on Linux fails
with "text file busy". Write to `path.new`, chmod, then `sudo mv -f` atomically.

**systemd key reload:** `systemctl enable --now` does not restart a running
service. Always use `systemctl enable && systemctl restart` after updating keys.

## Technical Decisions

- **Language:** Go — single binary, no runtime deps, same ecosystem as Coder/DevPod/Devbox
- **Config format:** YAML
- **Session manager:** zellij preferred, tmux supported
- **License:** Apache 2.0
- **Release tooling:** goreleaser
- **Windows host:** not supported (`hop-agent` is Linux-only; `hop` client supports Windows WSL)
