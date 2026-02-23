# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

Phase 0 complete, Phase 1 in progress. `hop setup`, `hop up`, `hop to`,
`hop upgrade` verified working. Reconnection monitoring, snapshot/restore,
and bridges all operational.

**ROADMAP.md** (repo root) is the living roadmap. When a feature is completed,
update the relevant checklist item in ROADMAP.md (change `[ ]` to `[x]`) and
move any newly implemented items into the correct phase.

## Build & Development Commands

```bash
# Build all binaries
make build

# Cross-compile agent for Linux (required for deployment)
CGO_ENABLED=0 GOOS=linux go build -o dist/hop-agent-linux ./cmd/hop-agent/...

# Dev workflow: upgrade all binaries from local builds
hop upgrade --local

# Dev workflow: use a local agent binary during hop setup
HOP_AGENT_BINARY=./dist/hop-agent-linux hop setup mybox -a <ip> -u <user> -k ~/.ssh/key

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

Three Go binaries in one monorepo:

```text
cmd/hop/        — Client CLI (macOS/Linux/Windows WSL)
cmd/hop-agent/  — Server daemon (Linux VPS, runs as systemd service)
cmd/hop-helper/ — Privileged helper daemon (macOS LaunchDaemon, handles TUN config + /etc/hosts)
```

**Communication:** Wireguard L3 tunnel (UDP) is the primary transport. SSH is used only for initial bootstrap (`hop setup`) and as an emergency fallback. The agent's control API listens at `<name>.hop:4200` — never exposed to the public internet.

**Client Wireguard mode:** Kernel TUN (utun on macOS). The hop-helper daemon (LaunchDaemon) handles privileged ops: TUN IP assignment, routing, /etc/hosts management. `hop up` creates the utun device (unprivileged on macOS), then delegates IP/route config to the helper via Unix socket at `/var/run/hopbox/helper.sock`.

**Hostname convention:** Each host gets `<name>.hop` added to `/etc/hosts` by the helper, so the agent is reachable as `mybox.hop:4200` from any process. No `DialContext` or proxy needed.

**Server Wireguard mode:** Kernel TUN (preferred, requires CAP_NET_ADMIN); netstack fallback if unavailable.

**No coordination server, no DERP relay.** The server is a public-IP VPS. Key exchange happens once over SSH during `hop setup`; all subsequent communication is over Wireguard.

**Netstack for `hop to` only:** `hop to` still uses a temporary netstack tunnel for migrating to a new host (avoids routing conflicts with the active kernel TUN). The netstack library stays available via `ClientTunnel`.

**Reconnection resilience:** The `hop up` process monitors agent connectivity with a 5-second heartbeat (`internal/tunnel.ConnMonitor`). If the agent becomes unreachable for 2+ consecutive checks, it prints a warning and updates the tunnel state file. When connectivity returns, it logs the outage duration. `hop status` shows `CONNECTED` and `LAST HEALTHY` fields from the state file. WireGuard handles tunnel re-establishment natively; the monitor only observes and reports.

To test reconnection resilience manually:

```bash
# 1. Bring up the tunnel
hop up

# 2. In another terminal, verify LAST HEALTHY advances every ~5s
watch -n 3 hop status   # LAST HEALTHY should always show ≤ ~5s ago

# 3. Block WireGuard UDP on the server to simulate an outage
ssh user@server "sudo iptables -A INPUT -p udp --dport 51820 -j DROP"
# hop up terminal should print within ~10s:
#   [HH:MM:SS] Agent unreachable — waiting for reconnection...
# hop status should show CONNECTED: no

# 4. Restore connectivity
ssh user@server "sudo iptables -D INPUT -p udp --dport 51820 -j DROP"
# hop up terminal should print:
#   [HH:MM:SS] Agent reconnected (was down for Xs)
# hop status should show CONNECTED: yes, LAST HEALTHY: a few seconds ago

# Alternatively, simulate a shorter outage by restarting the agent:
ssh user@server "sudo systemctl restart hop-agent"
# The agent takes a few seconds to restart; the monitor should detect the gap
# and recover automatically without any intervention.
```

## Key Library Choices

| Component | Library |
| ----------- | --------- |
| Wireguard protocol | `git.zx2c4.com/wireguard-go` |
| Wireguard config | `github.com/WireGuard/wgctrl-go` |
| Userspace networking | `gvisor.dev/gvisor/pkg/tcpip` (via wireguard-go netstack) |
| Key generation | `golang.org/x/crypto/curve25519` |
| SSH (bootstrap only) | `golang.org/x/crypto/ssh` |

Do **not** use Tailscale's magicsock/DERP, tsnet, or libp2p — these are explicitly excluded as unnecessary for point-to-point client↔VPS topology.

## Workspace Manifest (hopbox.yaml)

The user-facing config file placed in a project directory. Declares everything for a workspace: `packages` (backend: nix/apt/static), `services` (type: docker/native), `bridges` (clipboard, chrome CDP, xdg-open), `env`, `secrets`, `scripts`, `backup`, `editor`, `session`.

The `host:` field in `hopbox.yaml` pins which registered host to use for this workspace.

Config files:

- `~/.config/hopbox/hosts/<name>.yaml` — per-host (WireGuard keys, tunnel IPs, SSH endpoint, SSH host key)
- `~/.config/hopbox/config.yaml` — global user settings (`default_host`)

## Bridge System

Bridges fall into two categories:

1. **Just Wireguard routing** — any TCP/UDP service port is directly reachable at `10.hop.0.2:<port>`. No bridge code needed.
2. **True bridges** — resources that are inherently local: Chrome CDP (client→server direction), clipboard (bidirectional), xdg-open (server→client), notifications.

The bridge system implements only category 2.

## Port Binding

Service ports declared in `hopbox.yaml` are bound to the WireGuard IP (`10.10.0.2`) by default, not `0.0.0.0`. This keeps services private to the tunnel.

To intentionally expose a port publicly, use the 3-part Docker format with an explicit bind address:

- `"8080:80"` → bound to `10.10.0.2:8080` (tunnel only)
- `"8080"` → bound to `10.10.0.2:8080` (tunnel only)
- `"0.0.0.0:8080:80"` → bound to all interfaces (public)

## CLI Commands

```text
hop setup <name> -a <ip> [-u user] [-k keyfile] [-p port]
                                Bootstrap: install agent, exchange WG keys, save host config
hop up [workspace]              Bring up WireGuard tunnel + bridges + services
hop down                        Tear down tunnel (Ctrl-C in foreground mode)
hop status                      Show host config and agent health
hop code [path]                 Open VS Code connected to the workspace
hop run <script>                Execute named script from hopbox.yaml
hop services [ls|restart|stop]  Manage workspace services
hop logs [service]              Stream service logs
hop snap [create|restore|ls]    Manage workspace snapshots (restic backend)
hop to <newhost>                Migrate workspace to new host (snap → restore)
hop bridge [ls|restart]         Manage local-remote bridges
hop host [add|rm|ls|default]    Manage host registry; default shows/sets default host
hop upgrade [--version V] [--local] Upgrade hop binaries (client, helper, agent)
hop rotate [host]               Rotate WireGuard keys without full re-setup
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

**RPC client (`internal/rpcclient`):** `Call`/`CallAndPrint` use `<name>.hop`
hostnames. `CallWithClient` takes a custom `http.Client` for `hop to` (netstack).
`CopyTo` streams plain-text responses.

## Coding Conventions

- Error variables must always be named `err`. Never use suffixed names like `werr`, `rerr`, `cerr`, etc. Use shadowing or restructure to avoid conflicts.
- Service definitions are `service.Def` (not `ServiceDef`).
- `cmd/hop/main.go` is intentionally thin — commands live in separate files (`up.go`, `setup.go`, `status.go`, etc.).

## Known Pitfalls

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

**Key rotation atomicity:** Between `hop-agent` restart and the client config save,
keys are temporarily out of sync. The server keeps `agent.key.bak` for manual
recovery. The config save is a single `os.WriteFile` and is very unlikely to fail
after the agent restart succeeds.

**`os.WriteFile` on existing files:** `os.WriteFile` does not update permissions
on an existing file — the mode argument only applies when creating a new file.
If you `os.CreateTemp` (mode 0600) and then `os.WriteFile` to it, the file stays
0600. Always `os.Chmod` explicitly after writing if you need specific permissions.

**`hop services ls` only shows manifest-registered services:** The agent's service
manager only knows about services declared in `hopbox.yaml` and loaded at startup
(or via `workspace.sync`). Docker containers running independently on the host are
not visible — `hop services ls` will return "No services." if no manifest was loaded.

## Technical Decisions

- **Language:** Go — single binary, no runtime deps, same ecosystem as Coder/DevPod/Devbox
- **Config format:** YAML
- **Session manager:** zellij preferred, tmux supported
- **License:** Apache 2.0
- **Release tooling:** goreleaser
- **Windows host:** not supported (`hop-agent` is Linux-only; `hop` client supports Windows WSL)
