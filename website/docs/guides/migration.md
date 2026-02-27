---
sidebar_position: 6
---

# Migration

The `hop to` command migrates your workspace from one VPS to another. It snapshots the current workspace, bootstraps the new host, and restores the data — all in one command.

## Prerequisites

- An active tunnel to the source host (run `hop up` first)
- SSH access to the target host (same requirements as `hop setup`)
- A `backup` section in `hopbox.yaml` with a shared backend (e.g., S3) that both hosts can access

## Command

```bash
hop to <target-name> -a <ip> [-u user] [-k keyfile] [-p port]
```

The flags are the same as `hop setup`:

| Flag | Default | Description |
|------|---------|-------------|
| `<target-name>` | *(required)* | Name for the new host |
| `-a, --addr` | *(required)* | SSH IP of the new host |
| `-u, --user` | `root` | SSH username |
| `-k, --key` | *(auto-detected)* | SSH private key path |
| `-p, --port` | `22` | SSH port |

## Migration flow

Before starting, `hop to` shows a confirmation prompt:

```
Migrate workspace from mybox → newbox (5.6.7.8)?
  1. Create snapshot on mybox
  2. Bootstrap newbox via SSH
  3. Restore snapshot on newbox
  4. Set newbox as default host

Proceed? [y/N]
```

### Step 1: Snapshot

A snapshot is created on the source host via `snap.create`. This captures all service data directories. If the snapshot fails, no changes are made to the target.

### Step 2: Bootstrap

The target host is set up exactly like `hop setup` — agent binary uploaded, WireGuard keys exchanged, host config saved. The SSH connection uses trust-on-first-use for the new host's key.

### Step 3: Restore

Hopbox creates a temporary WireGuard tunnel to the target (using netstack to avoid routing conflicts with the active kernel tunnel). It probes the new agent's health, then sends the `snap.restore` RPC with the snapshot ID from step 1.

The temporary tunnel has a 5-minute timeout.

### Step 4: Switch default

The target host is set as the default. The migration is complete:

```
Migration complete. Run 'hop up' to connect.
```

## After migration

Run `hop up` to connect to your new host:

```bash
hop up
```

Your services, data, and configuration are restored on the new host.

## Error recovery

The migration is designed to be safe at each step. If it fails partway through:

| Failure at | State | How to recover |
|------------|-------|----------------|
| Snapshot | Source unchanged, target untouched | Retry `hop to` |
| Bootstrap | Snapshot exists on source | Retry `hop to` (overwrites partial setup) |
| Restore | Agent running, data not restored | `hop snap restore <id> --host <target>` |
| Switch | Data restored, wrong default | `hop host default <target>` |

The source host is never modified (beyond creating the snapshot). You can always fall back to the source by running `hop up` with `--host <source>`.
