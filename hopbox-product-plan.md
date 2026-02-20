# Hopbox — Product Direction & Development Plan

**Domain:** hopbox.dev
**CLI:** `hop`
**Alias commands:** `hop up`, `hop down`, `hop to`, `hop snap`, `hop bridge`, `hop shell`, `hop status`, `hop rotate`

---

## Identity

**One-liner:** A self-hostable workspace runtime that makes remote development feel local.

**Elevator pitch:** Hopbox is an open-source client-server tool that lets developers define, deploy, connect to, and migrate complete development workspaces on any Linux host. It goes beyond package managers and container runtimes by managing the full workspace lifecycle — toolchains, services, Wireguard networking, local-remote bridging, sessions, backups, and mobility — from a single declarative config.

---

## Who This Is For

### Primary: The Power-User Solo Developer

A developer who owns or rents a VPS (OVH, Hetzner, DigitalOcean, bare metal) and wants to develop remotely with the fluidity of working locally. They are comfortable with the terminal, manage their own infrastructure, and are frustrated that existing tools force them to choose between cloud lock-in (Codespaces), enterprise complexity (Coder), container-only workflows (DevPod), or toolchain-only management (Devbox).

**Profile:**
- Runs 1-3 personal or side-project workspaces
- Wants to SSH into a VPS and have everything just work
- Needs services (databases, k8s, queues) alongside their toolchain
- Wants their local browser, clipboard, and ports seamlessly bridged
- Cares about portability: move workspace between providers without rebuilding from scratch
- Doesn't want to deploy a control plane, Postgres database, or Terraform templates just to dev on a VPS

### Secondary: Small Teams (2-10 developers)

A startup or small team where one person (a lead or infra-minded dev) sets up shared workspace definitions that the rest of the team connects to. Not enterprise — no RBAC, SSO, audit logs needed. Just "clone the repo, run `hop up`, start coding."

### Tertiary (Future): Platform Teams Seeking a Lightweight CDE

Teams at mid-sized companies who find Coder too heavy and Codespaces too locked-in, and want a self-hosted CDE they can customize without Terraform expertise. This is a Phase 3+ audience.

---

## Problems We Solve

### Problem 1: Remote development doesn't feel local

**Pain:** When you develop on a remote host, you constantly bump into the seam between local and remote. Your clipboard doesn't cross the boundary. Your browser can't be automated by remote tools. Port forwarding is manual and fragile. Opening a link from the terminal doesn't open your local browser. Notifications from build tools don't reach your desktop.

**Our solution: Wireguard mesh + Bridge System.** Hopbox establishes a Wireguard tunnel between your laptop and workspace, creating a private L3 network where every port is directly reachable — no per-port SSH forwarding. On top of that, the bridge system transparently exposes local resources (browser CDP, clipboard, notifications) to the remote workspace. Run `hop up` and everything is wired up automatically.

**Nobody else does this.** Coder uses Wireguard (via Tailscale's stack) but only for raw connectivity — no bridge abstraction for local resources. DevPod connects your IDE but doesn't bridge anything else. Codespaces handles port forwarding within VS Code but nothing beyond that.

### Problem 2: Workspace definitions are incomplete

**Pain:** Devbox defines your toolchain. Docker Compose defines your services. SSH config defines your connection. Backup scripts define your persistence. These are four separate configs maintained separately, and none of them know about each other. When you set up a new machine or move to a new provider, you reassemble everything from memory.

**Our solution: The Workspace Manifest.** A single `hopbox.yaml` that declares everything: packages, services, bridges, environment variables, secrets, editor preferences, backup targets, and scripts. One file describes one workspace completely.

### Problem 3: Workspaces are not portable

**Pain:** Your dev environment accumulates state — database data, k8s cluster config, shell history, editor state, local tools. When your VPS dies, your provider doubles their prices, or you just want to move to a closer datacenter, you're rebuilding from scratch.

**Our solution: Workspace Mobility.** `hop snap` captures workspace state into a portable archive. `hop to new-host` provisions a fresh workspace on a new host and restores the snapshot. The manifest is the source of truth for what to install, and the snapshot covers the stateful data.

### Problem 4: Service orchestration in dev is either too simple or too complex

**Pain:** process-compose handles basic "start Postgres and Redis." Docker Compose handles multi-container setups but adds indirection. Neither handles a k3s cluster as a first-class workspace resource, or services with health checks, data persistence declarations, and restart policies.

**Our solution: Hybrid service management.** Hopbox supports services as native processes, Docker containers, or Kubernetes resources within a k3s cluster — all declared in the same manifest with health checks, data directories, dependency ordering, and log aggregation.

### Problem 5: The existing tools are all moving toward enterprise/AI and away from individual developers

**Pain:** Coder is building for Fortune 500 platform teams and AI agent governance. Gitpod became Ona and pivoted entirely to AI agents. Daytona pivoted to AI sandbox infrastructure. The individual developer who just wants a great remote dev experience is being abandoned.

**Our solution: Stay focused on the developer.** Hopbox is built for the person writing code, not the platform team managing 500 developers. The UX is a CLI you run on your laptop, not a dashboard you deploy.

---

## Competitive Positioning

```
                        Enterprise / Teams
                              │
                    Coder ●   │
                              │
                              │   ● DevZero
             Ona (Gitpod) ●   │
                              │
        ──────────────────────┼──────────────────────
        Container-only        │        Full Workspace
                              │
                   DevPod ●   │       ● Hopbox (us)
                              │
                  Devbox ●    │
          (toolchain-only)    │
                              │
                        Solo / Power-User
```

**We are bottom-right.** Full workspace management for the individual power-user. Nobody occupies this quadrant.

### Why not just use...

| Tool | What it does well | What it doesn't do |
|------|------------------|--------------------|
| Coder | Self-hosted CDEs, Terraform templates, enterprise governance | Simple setup for solo devs, local resource bridging, workspace mobility |
| DevPod | Client-only, devcontainer.json, multi-provider | Non-container workflows, bridges, services beyond Docker Compose, k8s-as-workspace |
| Devbox | Nix-powered reproducible toolchains | Services, connectivity, remote development, backups |
| Codespaces | Zero-friction from GitHub repos | Self-hosting, non-GitHub repos, local resource bridging |

---

## Architecture

### Components

```
┌──────────────────────┐                  ┌───────────────────────────┐
│   hop (client)        │   Wireguard      │    hop-agent (server)      │
│                      │   tunnel         │                           │
│  - Tunnel setup      │◄────────────────►│  - Workspace lifecycle    │
│  - Local bridges     │  10.hop.0.1       │  - Package installation   │
│  - Port auto-forward │  ◄──────────►    │  - Service orchestration  │
│  - Health TUI        │  10.hop.0.2       │  - Port monitoring        │
│  - Snapshot trigger  │                  │  - Snapshot/restore       │
│                      │  SSH (bootstrap  │  - Control API            │
│  Runs on: laptop     │   + fallback)    │                           │
│                      │                  │  Runs on: VPS / any Linux │
└──────────────────────┘                  └───────────────────────────┘
```

**hop (client CLI):** Go binary. Runs on macOS/Linux/Windows (WSL). Manages the Wireguard tunnel, launches local bridge daemons (Chrome, clipboard), provides a TUI for status monitoring, and triggers remote operations.

**hop-agent (server daemon):** Go binary. Runs on the remote host as a systemd service. Manages the workspace lifecycle: installs packages, starts/stops services, monitors ports, exposes a control API. Single binary, no external dependencies.

---

## Networking: Why Wireguard from Day One

### The SSH tunnel problem

With SSH port forwarding, every service port is a separate TCP tunnel:

```
Postgres 5432  →  ssh -L 5432:localhost:5432
Redis 6379     →  ssh -L 6379:localhost:6379
k3s 6443       →  ssh -L 6443:localhost:6443
Dev server 3000→  ssh -L 3000:localhost:3000
API 8080       →  ssh -L 8080:localhost:8080
Chrome CDP 9222→  ssh -R 9222:localhost:9222
Clipboard 2489 →  ssh -R 2489:localhost:2489
...
```

This creates compounding problems:
- **TCP-over-TCP:** Every tunneled TCP connection is wrapped in SSH's TCP. Under packet loss, the outer TCP retransmits while inner TCP also retransmits — exponential degradation.
- **Reconnection fragility:** All tunnels must be re-established on disconnect. One failed tunnel means partial connectivity.
- **Per-port management:** Adding a new service means adding a new tunnel. Auto-discovery requires dynamic tunnel creation, which SSH doesn't support cleanly.
- **No UDP support:** SSH tunnels are TCP-only. Can't forward DNS (53/udp), game server traffic, or other UDP services.
- **Resource overhead:** Each tunnel is a separate TCP connection with its own buffers, congestion window, and keepalive.

### The Wireguard solution

With Wireguard, client and server share a private L3 network:

```
Client:  10.hop.0.1/24   ◄──── Wireguard tunnel (single UDP stream) ────►   Server: 10.hop.0.2/24
         │                                                                          │
         └── Every port on 10.hop.0.2 is directly reachable ────────────────────────┘
```

- **No TCP-over-TCP:** Wireguard is UDP-based. TCP services inside the tunnel run over UDP, avoiding double-retransmission.
- **Single connection:** One UDP socket handles all traffic. No per-port management.
- **Survives network changes:** Wireguard is stateless — when your laptop switches from WiFi to mobile, the tunnel resumes instantly with no handshake delay.
- **Full L3 connectivity:** TCP, UDP, ICMP — everything works. `kubectl`, database tools, dev servers all just connect to 10.hop.0.2 directly.
- **Roaming built-in:** Client IP can change; Wireguard updates the endpoint on the next authenticated packet.
- **Near-zero overhead:** ~5% throughput cost, sub-1ms latency added. The kernel module does crypto at line speed; the Go userspace implementation is nearly as fast.

### What Wireguard simplifies in Hopbox

Bridges become cleaner with two distinct categories:

**Category 1: Just routing (handled by Wireguard automatically)**
- Database ports, API servers, k3s, dev servers, any TCP/UDP service
- No bridge code needed — they're reachable via Wireguard IP
- Auto-discovery becomes simple: agent monitors /proc/net/tcp, reports listening ports, client sees them immediately

**Category 2: True bridges (resources that are inherently local)**
- Chrome CDP: browser runs on your laptop, agent needs to reach it → reverse direction over Wireguard
- Clipboard: bidirectional sync daemon, one side on each end of tunnel
- Notifications: push from agent to client desktop
- File-open handler: `xdg-open` on server opens URL in local browser

The bridge system focuses entirely on Category 2. Port forwarding stops being a bridge concern — it's just IP routing.

### Implementation: Go Libraries & Tools

#### Core: wireguard-go + wgctrl-go

```
wireguard-go (git.zx2c4.com/wireguard-go)
├── device/     — Wireguard protocol implementation
├── conn/       — UDP socket abstraction (conn.Bind interface)
├── tun/        — TUN device abstraction
│   └── netstack/  — Userspace TCP/IP stack (no TUN device needed)
└── ipc/        — Control socket for wg(8) compatibility

wgctrl-go (github.com/WireGuard/wgctrl-go)
└── Programmatic configuration of Wireguard interfaces
    ├── wgctrl.New()         — create client
    ├── client.ConfigureDevice() — set peers, keys, endpoints
    └── client.Device()      — read current config
```

**wireguard-go** is the official userspace Wireguard implementation in Go, maintained by Jason Donenfeld (Wireguard author). It can run in two modes:

1. **Kernel TUN mode** (requires root/CAP_NET_ADMIN): Creates a real network interface. Other processes see it as a normal network interface. Best performance.

2. **Netstack mode** (no privileges needed): Uses gVisor's userspace TCP/IP stack. No kernel TUN device. The Wireguard tunnel is entirely in-process. This is what Tailscale and Coder use for their embedded implementations.

**wgctrl-go** is the Go library for configuring Wireguard interfaces programmatically — generating keys, adding peers, setting endpoints, managing allowed IPs. Works with both kernel and userspace implementations.

#### How Coder does it (reference architecture)

Coder's networking stack (open source, MIT-licensed) is built on Tailscale's fork of wireguard-go:

```
coder/tailnet/
├── conn.go        — Wraps Tailscale's netstack with Coder-specific config
├── coordinator.go — Manages peer discovery (who is connected where)
├── derp.go        — Embedded DERP relay for NAT traversal fallback
└── tunnel.go      — High-level tunnel management
```

Key decisions they made:
- Use **netstack** (userspace) mode — no root needed on client
- Embed a **DERP relay** in the Coder server for NAT traversal fallback
- Use Tailscale's **magicsock** for automatic path selection (direct UDP → DERP fallback)
- Peer coordination happens through the Coder control plane

#### Our approach (simpler than Coder)

We don't need Coder's full complexity because our topology is simpler: one client ↔ one server, where the server has a public IP (it's a VPS). This means:

```
┌─────────────────┐                           ┌──────────────────┐
│  hop client      │                           │  hop-agent        │
│                 │    UDP (Wireguard)         │                  │
│  wireguard-go   │◄─────────────────────────►│  wireguard-go    │
│  (netstack)     │                           │  (kernel TUN     │
│                 │    SSH (bootstrap only)    │   or netstack)   │
│  No root needed │──────────────────────────►│                  │
│                 │                           │  Has public IP   │
└─────────────────┘                           └──────────────────┘
```

**No DERP relay needed** — the server is a VPS with a public IP. The client always knows where to reach it. NAT traversal is one-directional (client behind NAT → server with public IP), which Wireguard handles natively via the roaming endpoint mechanism.

**No coordination server needed** — there's exactly one peer on each side. Key exchange happens once during `hop setup`.

**SSH is bootstrap only** — used for initial agent installation (`hop setup`) and as an emergency fallback. Day-to-day connectivity is Wireguard.

#### Key exchange & setup flow

```
$ hop setup mybox --host 203.0.113.10 --user gandalf

1. [SSH] Connect to host, install hop-agent binary
2. [SSH] hop-agent generates Wireguard keypair, stores private key
3. [SSH] hop-agent returns its public key + assigns tunnel IPs
4. [LOCAL] hop generates client Wireguard keypair
5. [SSH] hop sends client public key to agent
6. [SSH] hop-agent configures its Wireguard peer (client pubkey, allowed IPs)
7. [LOCAL] hop configures local Wireguard peer (agent pubkey, endpoint=203.0.113.10:51820, allowed IPs)
8. [WG] Tunnel is live. Verify with ping 10.hop.0.2
9. [WG] All subsequent hop commands use Wireguard for data, SSH for nothing

Stored in ~/.config/hopbox/hosts/mybox.yaml:
  name: mybox
  endpoint: 203.0.113.10:51820
  private_key: <client-wg-private-key>
  peer_public_key: <agent-wg-public-key>
  tunnel_ip: 10.hop.0.1
  agent_ip: 10.hop.0.2
```

#### Library selection summary

| Component | Library | Why |
|-----------|---------|-----|
| Wireguard protocol | `wireguard-go` (git.zx2c4.com/wireguard-go) | Official implementation, maintained by WG author, pure Go, supports netstack |
| WG configuration | `wgctrl-go` (github.com/WireGuard/wgctrl-go) | Official Go config library, key generation, peer management |
| Userspace networking | `gvisor.dev/gvisor/pkg/tcpip` (via wireguard-go/tun/netstack) | No-root networking, what Tailscale/Coder use in production |
| Key generation | `golang.org/x/crypto/curve25519` | Standard Wireguard key derivation |
| SSH (bootstrap) | `golang.org/x/crypto/ssh` | Initial setup and emergency fallback only |

#### What we explicitly skip (and why)

- **Tailscale's magicsock / DERP**: Needed for peer-to-peer mesh with both sides behind NAT. Our server has a public IP — unnecessary complexity.
- **tsnet (Tailscale as a library)**: Requires Tailscale coordination server (theirs or self-hosted Headscale). We don't want that dependency.
- **Full Tailscale integration**: Adds auth flow, ACLs, identity system. Overkill for one client ↔ one server. Could be an optional backend in Phase 3 for teams.
- **libp2p wireguard**: Research/experimental, not production-grade.

### Fallback strategy

If Wireguard UDP is blocked (strict corporate firewalls, hotel WiFi):

**Phase 0:** SSH fallback. `hop up --ssh` falls back to SSH tunneling with auto port forwarding from the manifest. Degraded experience but functional.

**Phase 2:** Wireguard-over-WebSocket. Wrap the Wireguard UDP packets in a WebSocket connection over HTTPS (port 443). This punches through nearly any firewall. Coder does this via their DERP relay; we can implement a simpler version since we only need point-to-point.

**Phase 3:** Optional DERP relay for team scenarios where both client and workspace might be behind NAT (e.g., workspace running on a teammate's homelab).

---

## Workspace Manifest (hopbox.yaml)

```yaml
workspace:
  name: gaming-platform

packages:
  backend: nix                  # or "apt", "brew", "static"
  list:
    - go@1.22
    - node@20
    - kubectl@1.30
    - k9s@latest
    - ripgrep@latest

services:
  postgres:
    type: docker
    image: postgres:16
    ports: [5432]
    env:
      POSTGRES_PASSWORD: dev
    data: ./data/postgres        # backed up by hop snap
    health: "pg_isready -U postgres"

  redis:
    type: docker
    image: redis:7-alpine
    ports: [6379]

  k3s:
    type: kubernetes
    version: "1.30"
    manifests: ./k8s/dev/        # applied on start
    ports: [6443]
    data: ./data/k3s

bridges:
  chrome:
    protocol: cdp
    direction: client-to-server  # expose local browser to remote
    local_port: 9222
    auto_launch: "google-chrome --remote-debugging-port=9222"

  clipboard:
    protocol: lemonade           # or built-in
    bidirectional: true

  open:
    protocol: xdg-open           # server xdg-open → opens locally
    direction: server-to-client

env:
  KUBECONFIG: /home/dev/.kube/config
  GOPATH: /home/dev/go

env_from: .env.dev

secrets:
  provider: sops
  file: secrets/dev.enc.yaml

scripts:
  dev: "hop run api & hop run frontend"
  api: "go run ./cmd/api"
  frontend: "cd frontend && npm run dev"
  test: "go test ./..."
  lint: "golangci-lint run"

backup:
  target: s3://my-bucket/workspaces/${workspace.name}
  schedule: "0 */6 * * *"
  include:
    - data/
    - .kube/
    - .config/
  engine: restic

editor:
  type: vscode-remote
  extensions:
    - golang.go
    - ms-kubernetes-tools.vscode-kubernetes-tools

session:
  manager: zellij
  default_layout: dev
```

---

## Development Plan

### Phase 0 — Dogfood & Skeleton (Weeks 1-4)

**Goal:** Replace your current manual SSH + tunnel workflow for the gaming platform. Wireguard tunnel from day one.

#### Milestone 0a: Wireguard tunnel works (Week 1-2)

- [x] **Go module scaffolding** — `cmd/hop/` (client) + `cmd/hop-agent/` (server) in one repo
- [x] **Wireguard key management** — generate keypairs using `wgctrl-go`, store in `~/.config/hopbox/`
- [x] **hop-agent Wireguard listener** — embed wireguard-go with kernel TUN mode, listen on UDP 51820, accept configured peer
- [x] **hop client Wireguard dialer** — embed wireguard-go with netstack mode (no root), connect to agent endpoint
- [x] **`hop setup <name> --host <ip> --user <user>`** — SSH to host, install hop-agent, exchange Wireguard keys, verify tunnel with ping
- [x] **`hop up`** — bring up Wireguard tunnel, verify connectivity to agent IP
- [x] **`hop down`** — tear down Wireguard tunnel cleanly
- [x] **`hop status`** — show tunnel state, latency, handshake time, bytes transferred

**Test:** `ping 10.hop.0.2` works. `curl 10.hop.0.2:8080` reaches a service on the VPS. No SSH tunnel involved.

#### Milestone 0b: Agent control + services (Week 2-3)

- [x] **hop-agent control API** — HTTP server on Wireguard IP (10.hop.0.2:4200), JSON-RPC. Only accessible over tunnel.
- [x] **hopbox.yaml parser** — Go struct definitions, YAML unmarshaling, basic validation
- [x] **Package installation** — shell-out to apt/nix based on backend field
- [x] **Docker service management** — start/stop containers from services section, docker CLI shell-out
- [x] **Port monitoring** — poll /proc/net/tcp, report listening ports to client via control API
- [x] **`hop services`** — list services, their status, ports
- [x] **`hop services restart <name>`** — restart a specific service

**Test:** `hop up` brings tunnel up, then `psql -h 10.hop.0.2 -U postgres` connects to the workspace Postgres directly over Wireguard. No port forwarding configured anywhere.

#### Milestone 0c: Bridges + shell (Week 3-4)

- [x] **Bridge: clipboard** — lemonade daemon on both sides, communicating over Wireguard. `pbcopy` on server routes to local clipboard and vice versa.
- [x] **Bridge: Chrome CDP** — on `hop up`, start Chrome with `--remote-debugging-port=9222`. Agent reaches it at 10.hop.0.1:9222 over Wireguard.
- [x] **`hop shell`** — SSH into workspace (uses Wireguard IP as SSH target, not public IP). Attach to zellij/tmux session.
- [x] **`hop run <script>`** — execute a script from hopbox.yaml scripts section on the remote
- [ ] **SSH fallback** — `hop up --ssh` when Wireguard is blocked, falls back to SSH tunneling

**Test:** Use Hopbox daily for gaming platform development. Chrome MCP server on VPS controls local browser. Clipboard works bidirectionally. `hop shell` drops into persistent zellij session.

### Phase 1 — Core Feature Set (Weeks 5-9)

**Goal:** Fully functional for a solo developer. This is the version you'd show to other people.

- [x] **Service orchestration improvements**
  - Dependency ordering (postgres starts before api)
  - Health check polling with configurable intervals
  - Log aggregation: `hop logs <service>` streams, `hop logs` streams all
  - Data directory declarations for backup awareness

- [ ] **k3s as a first-class service type**
  - hop-agent installs k3s if type: kubernetes declared
  - Auto-applies manifests from declared directory on start
  - kubectl just works: `kubectl --server https://10.hop.0.2:6443`

- [x] **Snapshot & restore**
  - `hop snap` — agent collects data directories, creates archive, pushes to S3 via restic
  - `hop snap restore <id>` — pulls snapshot, extracts, restarts services
  - Snapshot metadata: timestamp, hopbox.yaml hash, package versions

- [ ] **`hop to <newhost>`** — the killer command
  - Snapshot current workspace
  - Run `hop setup` on new host
  - Restore snapshot on new host
  - Verify services, switch DNS/config to new host
  - Interactive: shows transfer size, confirms before proceeding

- [ ] **Reconnection resilience**
  - Wireguard handles roaming natively — tunnel survives WiFi→mobile
  - Agent health heartbeat over Wireguard
  - Session survives disconnect (zellij/tmux)
  - Auto-reconnect for bridges on tunnel recovery

- [ ] **TUI status dashboard**
  - Wireguard tunnel health (latency, handshake age, bytes)
  - Service status (name, state, health, uptime)
  - Active bridges (clipboard, CDP, open handler)
  - Quick actions (restart service, reconnect bridge)

- [x] **Configuration management**
  - `~/.config/hopbox/config.yaml` for client defaults
  - Host registry: `hop host add mybox --address x.x.x.x --user gandalf`
  - `hop init` — generates hopbox.yaml scaffold

**Test:** Set up a second workspace for Nautilus. Both work independently. `hop to` one of them to a different VPS.

### Phase 2 — Open Source Release (Weeks 10-15)

**Goal:** Publishable, documented, installable by someone who isn't you.

- [ ] **Package management abstraction**
  - Backend interface: Install(pkg), Remove(pkg), List()
  - Backends: apt, nix (via devbox shell-out), static (binary URL)
  - Lock file: `hopbox.lock` for reproducibility

- [ ] **devcontainer.json compatibility (read-only)**
  - Map features→packages, forwardPorts→auto-accessible ports, postCreateCommand→scripts
  - "Imported from devcontainer.json. Run `hop init --full` to generate hopbox.yaml."

- [ ] **Wireguard-over-WebSocket fallback**
  - For corporate/hotel networks that block UDP
  - Wrap WG packets in WSS over port 443
  - Automatic detection and fallback

- [ ] **Installation & packaging**
  - `curl -fsSL https://hopbox.dev/install.sh | sh`
  - `hop setup` installs hop-agent + systemd unit on remote
  - Homebrew tap, AUR, goreleaser for GitHub releases

- [ ] **Documentation site (hopbox.dev)**
  - Quick start (5 minutes to first workspace)
  - Manifest reference
  - Bridge cookbook
  - Migration from DevPod / Devbox / manual SSH
  - Architecture deep-dive (networking, security)

- [ ] **GitHub repo**
  - Apache 2.0 license
  - CI: tests, linting, cross-compilation
  - Example hopbox.yaml files for common stacks

### Phase 3 — Community & Ecosystem (Weeks 16-25)

- [ ] **Plugin system for bridges**
  - Bridge interface: Setup(), Teardown(), HealthCheck()
  - Built-in: clipboard, CDP, open-handler, notification
  - User-defined: script or binary in hopbox.yaml

- [ ] **Multi-workspace management**
  - `hop list` — all workspaces across hosts
  - `hop switch <workspace>` — disconnect current, connect to another

- [ ] **Editor integrations**
  - VS Code extension: "Connect to Hopbox workspace"
  - Neovim plugin: workspace-aware statusline

- [ ] **hop-hub (optional control plane)**
  - Lightweight, SQLite-embedded, single binary
  - Web dashboard for small teams
  - Host provisioning via cloud APIs (OVH, Hetzner, DO)
  - Optional DERP relay for team scenarios behind NAT
  - Tailscale integration option (use existing tailnet instead of built-in Wireguard)

- [ ] **Secrets management**
  - Built-in sops/age integration
  - `hop secret set DB_PASSWORD`
  - Injected into workspace env on `hop up`

### Phase 4 — Sustainability (Month 6+)

**Possible revenue:**
- **hop-hub Cloud** — Hosted control plane. Free for 1 workspace, paid for teams.
- **Managed backup storage** — Encrypted snapshots. Pay per GB.
- **Priority support** — For companies adopting Hopbox for teams.
- **Workspace templates marketplace** — Curated stacks maintained by community.

---

## Command Reference

```
hop setup <name> --host <ip>    Bootstrap: install agent, exchange WG keys, verify tunnel
hop up [workspace]              Bring up Wireguard tunnel + bridges + services
hop down                        Tear down tunnel and bridges cleanly
hop status                      Show tunnel, services, bridges health
hop shell                       Drop into remote shell (zellij/tmux session)
hop run <script>                Execute named script from hopbox.yaml
hop services [ls|restart|stop]  Manage workspace services
hop logs [service]              Stream service logs
hop snap                        Snapshot workspace state to backup target
hop snap restore <id>           Restore from snapshot
hop to <newhost>                Migrate workspace to new host (THE killer command)
hop bridge [ls|restart]         Manage local-remote bridges
hop host [add|rm|ls|default]    Manage host registry
hop rotate [host]               Rotate WireGuard keys without full re-setup
hop init                        Generate hopbox.yaml scaffold
```

---

## Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Single binary, great SSH/WG libs, same as Coder/DevPod/Devbox |
| Networking | Wireguard (day one) | Full L3, no TCP-over-TCP, survives roaming, ~zero overhead |
| WG implementation | wireguard-go + wgctrl-go | Official, maintained by WG author, pure Go |
| Client WG mode | Netstack (userspace) | No root needed on developer laptop |
| Server WG mode | Kernel TUN (preferred) | Best performance; netstack fallback if no root |
| Config format | YAML | Industry standard for devops tooling |
| Service orchestration | Built-in | Tight integration with health checks, data dirs, hybrid types |
| Container runtime | Docker (optional) | Ubiquitous; not required for non-container services |
| Package backends | Pluggable (apt, nix, static) | Don't marry Nix — let users choose |
| Session manager | zellij preferred, tmux supported | Persistence is critical |
| License | Apache 2.0 | Permissive, enterprise-friendly |
| Build/release | goreleaser | Standard for Go CLI tools |
| SSH role | Bootstrap + emergency fallback | Not the primary transport |

---

## Security Model

Wireguard provides:
- **Authenticated encryption** — every packet is encrypted with ChaCha20-Poly1305 and authenticated with the peer's public key
- **No attack surface when silent** — Wireguard doesn't respond to unauthenticated packets (stealth)
- **Forward secrecy** — new session keys derived every 2 minutes
- **Identity pinning** — each peer is identified by Curve25519 public key

Hopbox adds:
- **Agent control API only on Wireguard IP** — not reachable from public internet
- **Per-workspace key rotation** — `hop rotate` regenerates the server WireGuard keypair without a full re-setup
- **No third-party coordination** — keys exchanged directly via SSH during setup, no Tailscale/cloud dependency
- **Secrets never in hopbox.yaml** — sops/age encrypted, decrypted only in agent memory

---

## Non-Goals (Explicitly Out of Scope)

- **Full Nix purity** — We use Nix as one backend, not as the foundation.
- **Browser-based IDE** — We connect your local editor. We don't host code-server.
- **AI agent hosting** — We're not building sandboxes. The market is going there; we're going the other direction.
- **Windows host support** — hop-agent targets Linux. Client runs anywhere.
- **GUI application** — CLI-first. TUI dashboard is the richest UI. hop-hub gets a web dashboard eventually.
- **Kubernetes operator** — hop-agent is a daemon on bare Linux. K8s is a resource it manages.
- **Mesh networking** — Point-to-point only (client ↔ server). Mesh is a Phase 3+ team feature.

---

## Success Metrics

### Phase 0-1 (Internal)
- You use Hopbox daily for all development work
- Time to `hop up` from cold: < 5 seconds (Wireguard handshake is ~1 RTT)
- Time to set up new workspace from scratch: < 15 minutes
- Time to `hop to` to new host: < 30 minutes
- Zero manual SSH tunnel management

### Phase 2 (Launch)
- 10 non-you users within first month
- 100 GitHub stars within first month
- 3 community-reported bug fixes merged

### Phase 3 (Growth)
- 1,000 GitHub stars
- 5 community-contributed bridges or workspace templates
- 1 paid hop-hub customer or sponsor

---

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Coder simplifies UX for solo devs | Medium | High | Bridge system is our moat. Coder's Terraform architecture makes simplification hard. |
| DevPod adds bridge/tunnel features | Medium | Medium | Our manifest is richer (services, k8s, backup). DevPod is container-only by design. |
| Wireguard UDP blocked on some networks | Medium | Medium | SSH fallback (Phase 0), WG-over-WebSocket (Phase 2). |
| wireguard-go API changes | Low | Low | Pin version, vendor if needed. API has been stable for years. |
| Scope creep from gaming platform | High | High | hopbox.yaml stays generic. Game-specific features go in templates. |
| Splitting focus between Hopbox and gaming platform | High | High | Phase 0 is dogfooding. The two feed each other. Separate after Phase 1. |
| Low adoption in crowded market | Medium | Medium | Position as "the self-hosted workspace for devs who own their infra." |

---

## Immediate Next Steps

1. **Create GitHub repo** — github.com/hopboxdev/hopbox (or your preferred org)
2. **Go module init** — `github.com/hopboxdev/hopbox`, `cmd/hop/` + `cmd/hop-agent/`
3. **Wireguard PoC** — client (netstack) ↔ server (kernel TUN), key exchange, ping works
4. **hopbox.yaml parser** — struct definitions, validation
5. **hop-agent control API** — HTTP on Wireguard IP, health endpoint
6. **hop setup + hop up** — end-to-end: install agent, exchange keys, tunnel up, status green
7. **Use it.** If it doesn't improve your daily workflow on the gaming platform within week 2, re-evaluate.
