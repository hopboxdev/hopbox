# Hopbox

Instant dev environments on your own VPS — no cloud accounts, no coordination
server, no monthly seat fee.

`hop setup` SSHes into any Linux server, installs a lightweight agent, and
exchanges WireGuard keys. `hop up` brings up an encrypted peer-to-peer tunnel
from your laptop directly to that server. Everything the agent exposes —
services, scripts, backups, a remote shell — is reachable over the tunnel at
`10.10.0.2`.

```text
your laptop  ──── WireGuard UDP ────  your VPS
  hop (CLI)                           hop-agent (systemd)
  10.10.0.1                           10.10.0.2
```

No DERP relay. No Tailscale. No NAT traversal magic needed — the VPS just needs
a public IP and an open UDP port (default 51820).

---

## Requirements

| | Minimum |
| --- | --- |
| **Developer machine** | macOS or Linux (Windows WSL untested) |
| **VPS** | Any Linux with systemd and a public IP |
| **SSH access** | Key-based auth to the VPS |

---

## Quick start

### 1. Install `hop`

Download the latest binary for your platform from the
[releases page](https://github.com/hopboxdev/hopbox/releases), or build from
source:

```bash
go install github.com/hopboxdev/hopbox/cmd/hop@latest
```

### 2. Bootstrap your VPS

```bash
hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/id_ed25519
```

This will:

- SSH into the server and install `hop-agent` as a systemd service
- Generate WireGuard keys on both ends and exchange public keys
- Save the host config to `~/.config/hopbox/hosts/mybox.yaml`
- Set `mybox` as the default host

### 3. Bring up the tunnel

```bash
hop up
```

The tunnel is up when you see `Agent is up.` If a `hopbox.yaml` exists in the
current directory it is synced to the agent automatically.

---

## hopbox.yaml

Place a `hopbox.yaml` in your project directory to declare everything your
workspace needs:

```yaml
name: myapp
host: mybox          # which registered host to use (optional if default is set)

packages:
  - name: ripgrep
    backend: apt

services:
  postgres:
    type: docker
    image: postgres:16
    ports: [5432]
    env:
      POSTGRES_PASSWORD: secret
    data:
      - host: /data/postgres
        container: /var/lib/postgresql/data
    health:
      http: http://localhost:5432
      interval: 5s
      timeout: 30s

bridges:
  - type: clipboard   # sync clipboard between laptop and VPS

scripts:
  migrate: psql $DATABASE_URL -f schema.sql
  seed:    psql $DATABASE_URL -f seed.sql

backup:
  backend: restic
  target: s3://mybucket/myapp

session:
  manager: zellij
  name: myapp
```

Service ports are bound to the WireGuard IP (`10.10.0.2`) by default, so they
are only reachable through the tunnel. To expose a port publicly, use the
3-part Docker format: `"0.0.0.0:8080:80"`.

Generate a scaffold with `hop init`.

---

## Commands

```text
hop setup <name> -a <ip>        Bootstrap a VPS — install agent, exchange WG keys
hop up                          Bring up WireGuard tunnel + bridges + services
hop down                        Tear down tunnel (or press Ctrl-C in hop up)
hop status                      Show host info and agent health
hop shell                       Open a remote shell (attaches to zellij/tmux session)
hop run <script>                Run a named script from hopbox.yaml on the VPS
hop services ls                 List services and their status
hop services restart <name>     Restart a service
hop services stop <name>        Stop a service
hop logs <service>              Stream logs for a service
hop snap create                 Create a restic snapshot of service data
hop snap restore <id>           Restore from a snapshot
hop snap ls                     List snapshots
hop to <newhost>                Migrate workspace to a different VPS
hop host ls                     List registered hosts (* = default)
hop host default [name]         Show or set the default host
hop host rm <name>              Remove a host
hop init                        Generate a hopbox.yaml scaffold
hop version                     Print version
```

**Global flags:** `-H <name>` / `--host <name>` to override the host for any
command.

**Host resolution order:** `--host` flag → `host:` in `hopbox.yaml` →
`default_host` in `~/.config/hopbox/config.yaml`.

---

## Architecture

Two Go binaries live in this repo:

```text
cmd/hop/         Client CLI (macOS / Linux)
cmd/hop-agent/   Server daemon (Linux VPS, systemd service)
```

**Transport:** WireGuard L3 tunnel (UDP). SSH is used only during `hop setup`.

**Client WireGuard mode:** Userspace netstack (wireguard-go + gVisor) — no root
required on the developer machine.

**Server WireGuard mode:** Kernel TUN (`wg0`, requires `CAP_NET_ADMIN`) with a
userspace fallback.

**Agent control API:** HTTP/JSON-RPC on `10.10.0.2:4200`, reachable only over
the tunnel.

---

## Development

```bash
# Build
make build

# Test
go test ./...

# Lint
golangci-lint run

# Use a local agent binary during hop setup (skips GitHub release download)
HOP_AGENT_BINARY=./dist/hop-agent-linux hop setup mybox -a <ip> -u <user> -k ~/.ssh/key
```

Releases are cut with [goreleaser](https://goreleaser.com). Binaries are
published to GitHub Releases; `hop setup` downloads the matching `hop-agent`
binary automatically.

---

## License

GNU Affero General Public License v3.0 (AGPL-3.0).
