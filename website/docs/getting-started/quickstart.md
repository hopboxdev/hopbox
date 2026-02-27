---
sidebar_position: 2
---

# Quickstart

This guide walks you through setting up Hopbox in under 5 minutes.

## 1. Install hop

Follow the [installation guide](./installation.md) to install the `hop` CLI. The quickest method:

```bash
curl -fsSL https://get.hopbox.dev | sh
```

## 2. Bootstrap your VPS

Point `hop setup` at any Linux VPS with a public IP and SSH key access:

```bash
hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/id_ed25519
```

This command:

1. Connects via SSH (you'll confirm the host key fingerprint on first use)
2. Installs `hop-agent` as a systemd service on the remote host
3. Generates and exchanges WireGuard keys
4. Saves the host configuration to `~/.config/hopbox/hosts/mybox.yaml`
5. Sets `mybox` as your default host (if no default exists)

On the first run, it also installs the helper daemon locally (requires `sudo` once).

## 3. Bring up the tunnel

```bash
hop up
```

The tunnel starts as a background daemon. You'll see output like:

```
Bringing up tunnel to mybox (1.2.3.4)
Interface utun7 up, 10.10.0.1 → mybox.hop
Agent probe... healthy
Agent is up.
```

Your VPS is now reachable at `mybox.hop`. Every port on the server is directly accessible through the WireGuard tunnel — no SSH port forwarding needed.

## 4. Create a workspace (optional)

Create a `hopbox.yaml` in your project directory to declare packages, services, and bridges:

```yaml
name: myproject
host: mybox

packages:
  - name: nodejs
    backend: nix
  - name: postgresql
    backend: apt

services:
  postgres:
    type: docker
    image: postgres:16
    ports:
      - "5432"
    env:
      POSTGRES_PASSWORD: dev
    data:
      - host: /opt/hopbox/data/postgres
        container: /var/lib/postgresql/data

bridges:
  - type: clipboard
```

Then run `hop up` again to sync the manifest:

```bash
hop up
```

Hopbox installs the declared packages, starts the services, and activates bridges.

## 5. Check status

```bash
hop status
```

This shows a dashboard with your tunnel state, connection health, and forwarded ports.

## Next steps

- [Setup details](../guides/setup.md) — deep dive into `hop setup` and host configuration
- [Workspace lifecycle](../guides/workspace-lifecycle.md) — `hop up`, `hop down`, reconnection, daemon mode
- [Services](../guides/services.md) — Docker and native service management
- [Bridges](../guides/bridges.md) — clipboard, Chrome DevTools, and more
- [Snapshots](../guides/snapshots.md) — backup and restore workspaces
- [Migration](../guides/migration.md) — move your workspace to a new host
