# Managed Hopbox Product — Design

## Vision

Managed Hopbox is a paid product where customers get instant dev environments on Hopbox-owned bare metal. Each workspace is an isolated Firecracker microVM (via Silo) with WireGuard connectivity directly to the customer's laptop.

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| VM isolation | Firecracker via Silo | Multi-tenant security, resource control, snapshotting |
| WireGuard placement | Inside the VM (B1) | VM is self-contained, portable between hosts, end-to-end encryption |
| Auth | GitHub OAuth | Developer-native, workspace tied to repos |
| Billing | Stripe, usage-tracked | Pricing model TBD, but infrastructure tracks usage from day one |
| Workspace config | `.hopbox.yaml` in repo | Same format as self-hosted, declarative |
| Self-hosted mode | No Silo, direct to VPS | Keeps self-hosted simple, no unnecessary virtualization overhead |

## Architecture

```
Customer laptop            Hopbox bare metal              Hopbox control plane
┌─────────┐   WireGuard     ┌──────────────┐              ┌──────────────┐
│ hop CLI  │◄───────────────►│ Firecracker  │              │ Web API      │
│          │   direct UDP    │ VM           │              │ + Dashboard  │
└────┬─────┘                 │ (hop-agent)  │              │              │
     │                       └──────────────┘              │ - GitHub OAuth│
     │                       ┌──────────────┐              │ - Stripe     │
     │   HTTPS               │ Host agent   │◄─────────────│ - Workspace  │
     └──────────────────────►│ (Silo)       │  manages     │   CRUD       │
                             └──────────────┘              │ - Usage      │
                                                           └──────────────┘
```

### Connectivity flow

1. Customer runs `hop up` in a repo with `.hopbox.yaml`
2. CLI authenticates against control plane (GitHub OAuth)
3. Control plane selects a host, tells host agent to create VM via Silo
4. Host agent creates VM, injects `hop-agent` + WireGuard, generates keys
5. Host adds UDP port forward: `host:assigned_port → VM:51820`
6. Control plane returns WireGuard endpoint + keys to CLI
7. CLI establishes WireGuard tunnel directly to VM
8. VM is the workspace — same experience as a VPS

### What runs where

| Component | Location | Responsibility |
|-----------|----------|---------------|
| `hop` CLI | Customer laptop | Auth, tunnel, bridges, port forwarding |
| Control plane | Hosted (your infra) | Customer management, billing, host orchestration |
| Host agent | Each bare metal server | VM lifecycle via Silo, UDP port forwarding |
| `hop-agent` | Inside each Firecracker VM | WireGuard endpoint, workspace services, exec |
| Silo | Library used by host agent | VM create/destroy/suspend/resume/snapshot |

## Implementation Plan — 4 Steps

### Step 1: Prove the core (current focus)

**Goal:** Manually demonstrate that a laptop can WireGuard-connect to a `hop-agent` running inside a Silo Firecracker VM.

**What to prove:**
- `silo.Create()` spins up a VM
- `vm.Exec()` (vsock) installs `hop-agent` + `wireguard-go` inside the VM
- WireGuard keys generated inside the VM
- Host iptables forwards UDP port to VM's TAP IP
- `hop up` from laptop connects through WireGuard to the VM
- `hop-agent` API (`/health`, RPC) is reachable over the tunnel
- Services declared in `.hopbox.yaml` are accessible (e.g., postgres on port 5432)

**Risks to validate:**
- Firecracker guest kernel supports TUN devices (needed for WireGuard)
- WireGuard performance through TAP + UDP forwarding is acceptable
- Suspend/resume preserves WireGuard state (or reconnects cleanly)

**Deliverable:** A Go script or test that does the full setup, and a working `hop up` connection from a laptop.

### Step 2: Host agent

**Goal:** A daemon running on each bare metal server that exposes an authenticated API for VM lifecycle management.

**What to build:**
- gRPC or HTTP API: create workspace, destroy, suspend, resume, list
- Wraps Silo library for VM operations
- Handles WireGuard key provisioning inside VMs
- Manages UDP port allocation and iptables forwarding rules
- Reports host capacity and VM status to control plane
- Usage metering: track VM uptime, resource consumption

**Deliverable:** A `hopbox-host` binary that runs as a systemd service on bare metal.

### Step 3: Control plane

**Goal:** Web API + dashboard for customer and host management.

**What to build:**
- GitHub OAuth signup/login
- Stripe integration for payment methods and usage billing
- Workspace CRUD: create, list, delete (delegates to host agents)
- Host registry: track which bare metal servers are available, their capacity
- Scheduler: pick the best host for a new workspace (region, capacity)
- Usage aggregation: collect metering data from host agents
- Dashboard: workspace list, billing, account settings

**Deliverable:** A web service (likely Go + Postgres) deployed to your infra, plus a minimal dashboard.

### Step 4: CLI changes

**Goal:** Modify `hop` CLI to work with the managed product.

**What to change:**
- `hop login` — GitHub OAuth device flow against control plane
- `hop up` — if no local host config exists, call control plane to provision/connect
- `hop create` / `hop destroy` — explicit workspace management via control plane
- `hop billing` — usage and plan info
- Keep self-hosted mode working unchanged (SSH-based, no Silo)

**Deliverable:** Updated `hop` binary that supports both self-hosted and managed modes.

## What stays the same

- `.hopbox.yaml` manifest format — identical for self-hosted and managed
- `hop up` / `hop down` / `hop status` — same UX
- WireGuard tunnel — same protocol, same bridges, same port forwarding
- `hop-agent` inside the workspace — same binary, same API

## What's different from self-hosted

| Aspect | Self-hosted | Managed |
|--------|------------|---------|
| Target | User's VPS (bare metal or cloud VM) | Hopbox-owned bare metal |
| Isolation | None (full VPS access) | Firecracker VM via Silo |
| Provisioning | `hop setup` via SSH | Control plane API |
| Auth | SSH keys | GitHub OAuth |
| Billing | None (user pays for VPS) | Stripe usage-based |
| Multi-tenant | No | Yes |
