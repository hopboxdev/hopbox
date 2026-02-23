# Hopbox — Product Overview

## Identity

**One-liner:** A self-hostable workspace runtime that makes remote development feel local.

**Elevator pitch:** Hopbox is an open-source client-server tool that lets developers
define, deploy, connect to, and migrate complete development workspaces on any Linux
host. It goes beyond package managers and container runtimes by managing the full
workspace lifecycle — toolchains, services, Wireguard networking, local-remote bridging,
sessions, backups, and mobility — from a single declarative config.

---

## Who This Is For

### Primary: The Power-User Solo Developer

A developer who owns or rents a VPS (OVH, Hetzner, DigitalOcean, bare metal) and wants
to develop remotely with the fluidity of working locally. They are comfortable with the
terminal, manage their own infrastructure, and are frustrated that existing tools force
them to choose between cloud lock-in (Codespaces), enterprise complexity (Coder),
container-only workflows (DevPod), or toolchain-only management (Devbox).

**Profile:**
- Runs 1-3 personal or side-project workspaces
- Wants to SSH into a VPS and have everything just work
- Needs services (databases, k8s, queues) alongside their toolchain
- Wants their local browser, clipboard, and ports seamlessly bridged
- Cares about portability: move workspace between providers without rebuilding from scratch
- Doesn't want to deploy a control plane, Postgres database, or Terraform templates just to dev on a VPS

### Secondary: Small Teams (2-10 developers)

A startup or small team where one person (a lead or infra-minded dev) sets up shared
workspace definitions that the rest of the team connects to. Not enterprise — no RBAC,
SSO, audit logs needed. Just "clone the repo, run `hop up`, start coding."

### Tertiary (Future): Platform Teams Seeking a Lightweight CDE

Teams at mid-sized companies who find Coder too heavy and Codespaces too locked-in,
and want a self-hosted CDE they can customize without Terraform expertise. Phase 3+ audience.

---

## Problems We Solve

### Problem 1: Remote development doesn't feel local

**Pain:** When you develop on a remote host, you constantly bump into the seam between
local and remote. Your clipboard doesn't cross the boundary. Your browser can't be
automated by remote tools. Port forwarding is manual and fragile. Opening a link from the
terminal doesn't open your local browser.

**Our solution: Wireguard tunnel + Bridge System.** Hopbox establishes a Wireguard tunnel
between your laptop and workspace, creating a private L3 network where every port is
directly reachable — no per-port SSH forwarding. The bridge system transparently exposes
local resources (browser CDP, clipboard, notifications) to the remote workspace. Run
`hop up` and everything is wired up automatically.

### Problem 2: Workspace definitions are incomplete

**Pain:** Devbox defines your toolchain. Docker Compose defines your services. SSH config
defines your connection. Backup scripts define your persistence. Four separate configs
maintained separately, none of them aware of each other.

**Our solution: The Workspace Manifest.** A single `hopbox.yaml` that declares everything:
packages, services, bridges, environment variables, secrets, editor preferences, backup
targets, and scripts. One file describes one workspace completely.

### Problem 3: Workspaces are not portable

**Pain:** Your dev environment accumulates state — database data, k8s cluster config,
shell history, editor state, local tools. When your VPS dies or you want to move to a
closer datacenter, you're rebuilding from scratch.

**Our solution: Workspace Mobility.** `hop snap` captures workspace state into a portable
archive. `hop to new-host` provisions a fresh workspace on a new host and restores the
snapshot. The manifest is the source of truth for what to install, and the snapshot covers
the stateful data.

### Problem 4: Service orchestration in dev is either too simple or too complex

**Pain:** process-compose handles basic scenarios. Docker Compose adds indirection.
Neither handles services with health checks, data persistence declarations, and restart
policies alongside native processes in a single config.

**Our solution: Hybrid service management.** Hopbox supports services as Docker containers
or native processes — all declared in the same manifest with health checks, data directories,
dependency ordering, and log aggregation.

### Problem 5: Existing tools are moving away from individual developers

**Pain:** Coder is building for Fortune 500 platform teams. Gitpod became Ona and pivoted
to AI agents. Daytona pivoted to AI sandbox infrastructure. The individual developer who
just wants a great remote dev experience is being abandoned.

**Our solution: Stay focused on the developer.** Hopbox is built for the person writing
code, not the platform team managing 500 developers. The UX is a CLI you run on your
laptop, not a dashboard you deploy.

---

## Competitive Positioning

```
                        Enterprise / Teams
                              |
                    Coder *   |
                              |
                              |   * DevZero
             Ona (Gitpod) *   |
                              |
        ----------------------+----------------------
        Container-only        |        Full Workspace
                              |
                   DevPod *   |       * Hopbox (us)
                              |
                  Devbox *    |
          (toolchain-only)    |
                              |
                        Solo / Power-User
```

**We are bottom-right.** Full workspace management for the individual power-user.
Nobody else occupies this quadrant.

### Why not just use...

| Tool | What it does well | What it doesn't do |
|------|------------------|--------------------|
| Coder | Self-hosted CDEs, Terraform templates, enterprise governance | Simple setup for solo devs, local resource bridging, workspace mobility |
| DevPod | Client-only, devcontainer.json, multi-provider | Non-container workflows, bridges, services beyond Docker Compose |
| Devbox | Nix-powered reproducible toolchains | Services, connectivity, remote development, backups |
| Codespaces | Zero-friction from GitHub repos | Self-hosting, non-GitHub repos, local resource bridging |

---

## Architecture

```
+----------------------+                  +---------------------------+
|   hop (client)       |   Wireguard      |    hop-agent (server)     |
|                      |   tunnel         |                           |
|  - Tunnel setup      |<--------------->|  - Workspace lifecycle    |
|  - Local bridges     |  10.10.0.1       |  - Package installation   |
|  - Health TUI        |  <---------->    |  - Service orchestration  |
|  - Snapshot trigger  |  10.10.0.2       |  - Port monitoring        |
|                      |                  |  - Snapshot/restore       |
|  Runs on: laptop     |  SSH (bootstrap  |  - Control API            |
|                      |   + fallback)    |                           |
+----------------------+                  |  Runs on: VPS / any Linux |
                                          +---------------------------+

hop-helper (macOS LaunchDaemon)
  - TUN device creation (utun)
  - IP/route configuration
  - /etc/hosts management
  - Runs as: privileged root process
```
