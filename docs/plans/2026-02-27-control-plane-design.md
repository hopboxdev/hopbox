# Control Plane — Design

## Overview

The control plane (`hopbox-cp`) is a Go API server + embedded React dashboard that manages the managed Hopbox product. It handles user authentication (GitHub OAuth), workspace lifecycle (delegating to hostd via gRPC), and provides a minimal web dashboard for visibility.

This is the MVP: no billing, no multi-host scheduling, no RBAC. Single host, single binary, simple ops.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Unified Go binary | Single artifact, embeds React SPA via `embed.FS`, simple deployment |
| Frontend | React + shadcn + TypeScript | Rich component library, familiar tooling |
| Database | PostgreSQL | Standard relational DB, strong Go ecosystem (pgx) |
| Host auth | mTLS | Industry standard for service-to-service, native gRPC support, no shared secrets |
| Repo | Separate private repo | Licensing separation (open source CLI vs proprietary control plane) |
| Deployment | Separate VPS | Decoupled from bare metal hosts |
| Scale | 1 host to start | Simplifies scheduler (no scheduling needed) |

## System Architecture

```
                         Internet
                            │
                    ┌───────┴───────┐
                    │  Control VPS  │
                    │               │
                    │  hopbox-cp    │  ← single Go binary
                    │  ┌──────────┐ │
                    │  │ HTTP API │ │  ← /api/* JSON endpoints
                    │  │ + React  │ │  ← /* embedded SPA
                    │  │ + OAuth  │ │  ← GitHub device flow
                    │  └────┬─────┘ │
                    │       │       │
                    │  PostgreSQL   │
                    └───────┬───────┘
                            │ gRPC + mTLS
                    ┌───────┴───────┐
                    │  Bare Metal   │
                    │               │
                    │  hopbox-hostd │
                    │  ┌──────────┐ │
                    │  │ VMs      │ │
                    │  └──────────┘ │
                    └───────────────┘
```

### Components

- **`hopbox-cp`** — single Go binary on the control VPS. Serves the React dashboard, JSON API, and GitHub OAuth. Connects to hostd via gRPC over mTLS.
- **PostgreSQL** — runs on the same control VPS. Stores users, workspaces, host registry.
- **`hopbox-hostd`** — already built, runs on bare metal. Control plane is a gRPC client to it.

### Connectivity

- Users access the dashboard via HTTPS (Caddy or certbot for TLS).
- `hop` CLI authenticates against the API via GitHub OAuth device flow.
- Control plane calls hostd's existing gRPC API (Create/Destroy/Suspend/Resume/Get/List/HostStatus).
- Customer WireGuard traffic goes directly to the bare metal host (not through the control plane).

## Data Model

### `users`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK |
| github_id | BIGINT | unique |
| github_login | TEXT | |
| email | TEXT | nullable |
| avatar_url | TEXT | |
| created_at | TIMESTAMPTZ | |

### `hosts`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK |
| name | TEXT | unique, human-friendly |
| grpc_endpoint | TEXT | e.g. `10.0.0.5:9090` |
| public_ip | TEXT | for WireGuard endpoint |
| status | TEXT | online / offline / draining |
| total_vcpus | INT | |
| available_vcpus | INT | |
| created_at | TIMESTAMPTZ | |

### `workspaces`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK |
| user_id | UUID | FK → users |
| host_id | UUID | FK → hosts |
| name | TEXT | unique per user |
| state | TEXT | creating / running / suspended / destroyed |
| vcpus | INT | |
| memory_mb | INT | |
| disk_gb | INT | |
| host_port | INT | UDP port on host |
| vm_ip | TEXT | VM TAP IP |
| client_config | JSONB | private key, peer public key, endpoint, tunnel IPs |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

### `sessions`

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT | PK, the session token |
| user_id | UUID | FK → users |
| expires_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ | |

`client_config` is JSONB so the API can return the full WireGuard config to the CLI without re-deriving it from hostd.

## API Endpoints

### Auth

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/auth/github` | Initiate GitHub OAuth device flow, returns device_code + user_code |
| POST | `/api/auth/github/callback` | Poll for token exchange, returns session token |
| GET | `/api/auth/me` | Current user info |
| POST | `/api/auth/logout` | Invalidate session |

### Workspaces

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/workspaces` | Create workspace (name, image, vcpus, memory, disk). Calls hostd CreateWorkspace |
| GET | `/api/workspaces` | List user's workspaces |
| GET | `/api/workspaces/:id` | Get workspace details + client config |
| DELETE | `/api/workspaces/:id` | Destroy workspace. Calls hostd DestroyWorkspace |
| POST | `/api/workspaces/:id/suspend` | Suspend workspace |
| POST | `/api/workspaces/:id/resume` | Resume workspace |

### Hosts (admin only)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/hosts` | List registered hosts + capacity |
| POST | `/api/hosts` | Register a new host (endpoint, public IP) |
| DELETE | `/api/hosts/:id` | Deregister host |

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Control plane health check |

Auth via `Authorization: Bearer <session-token>` header. Admin endpoints gated by allowlist of GitHub user IDs in config.

## CLI Changes

Changes to `hop` CLI in the hopbox repo:

### New commands

**`hop login`** — GitHub OAuth device flow against control plane.
- Calls `POST /api/auth/github`, displays user code, opens browser.
- Polls callback until authorized.
- Stores session token + control plane URL in `~/.config/hopbox/auth.json`.
- Endpoint set via `hop login --endpoint https://cp.hopbox.dev`.

**`hop create <name>`** — create a managed workspace.
- Calls `POST /api/workspaces`.
- Saves returned client config as host config at `~/.config/hopbox/hosts/<name>.yaml` with `managed: true`.

**`hop destroy <name>`** — destroy a managed workspace.
- Calls `DELETE /api/workspaces/:id`.
- Removes local host config.

### Modified commands

**`hop up`** — detect managed vs self-hosted.
- If host config has `managed: true`, skip SSH bootstrap.
- Everything else unchanged (WireGuard tunnel, bridges, services).

No changes to `hop down`, `hop status`, `hop exec`, etc. They work against the agent API over the tunnel, which is identical for managed and self-hosted.

## Dashboard

React + shadcn + TypeScript. Three pages:

### Login page
- GitHub OAuth web flow (not device flow — that's for CLI).
- "Sign in with GitHub" button → GitHub redirect → back to dashboard.

### Workspaces page (main view)
- Table: name, state, vCPUs, memory, host port, created time.
- "Create Workspace" button → modal with name, image, vCPUs, memory, disk.
- Row actions: Suspend, Resume, Destroy (with confirmation).
- State as colored badges (running=green, suspended=yellow, creating=blue, destroyed=gray).
- Connection info expandable per row — shows `hop create` + `hop up` commands.

### Hosts page (admin only)
- Table: name, endpoint, public IP, status, capacity (used/total vCPUs).
- "Add Host" button → modal with endpoint + public IP.
- Status from periodic `HostStatus` gRPC calls.

## mTLS Setup

### Private CA
- Script or Makefile target generates root CA key + cert.
- CA cert deployed to both control VPS and bare metal hosts.
- Stored at `/etc/hopbox/ca.crt`.

### Host cert
- Generated per bare metal server: `hopbox-ca issue --host <hostname>`.
- Installed alongside hostd.
- hostd configured with `--tls-cert`, `--tls-key`, `--tls-ca` flags.
- Requires client certs signed by the CA.

### Control plane cert
- Generated once: `hopbox-ca issue --client controlplane`.
- `hopbox-cp` uses these when dialing hostd's gRPC endpoint.
- Configured via `--hostd-cert`, `--hostd-key`, `--hostd-ca` flags.

For 1 host: generate CA, issue 2 certs, point both services at them. No rotation automation needed yet.
