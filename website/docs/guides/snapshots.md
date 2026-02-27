---
sidebar_position: 5
---

# Snapshots

Hopbox uses [restic](https://restic.net) for workspace snapshots. Snapshots capture service data directories and can be restored on the same host or a different one (for migration).

## Configuration

Add a `backup` section to `hopbox.yaml`:

```yaml
backup:
  backend: restic
  target: s3:s3.amazonaws.com/mybucket/workspace
```

The `target` field is a restic repository URL. Supported backends include:

| Backend | Target format |
|---------|--------------|
| S3 | `s3:s3.amazonaws.com/bucket/path` |
| B2 | `b2:bucket:path` |
| Local | `local:/path/to/repo` |
| SFTP | `sftp:user@host:/path` |

### Credentials

Restic credentials are passed through environment variables. Set them in `.env.local` next to your `hopbox.yaml`:

```bash
RESTIC_PASSWORD=your-repo-password
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
```

## Creating a snapshot

```bash
hop snap create
```

This tells the agent to run a restic backup of all service data directories declared in `hopbox.yaml`. The output includes the snapshot ID:

```
Snapshot created: a1b2c3d4
```

Each snapshot is tagged with a manifest hash so you can identify which workspace configuration it belongs to.

## Listing snapshots

```bash
hop snap ls
```

Displays a table of available snapshots:

```
ID        CREATED
a1b2c3d4  2 hours ago
e5f6g7h8  3 days ago
```

## Restoring a snapshot

```bash
hop snap restore a1b2c3d4
```

This restores the snapshot contents to their original paths on the server. Service data directories are overwritten with the snapshot data.

You can optionally specify a restore path:

```bash
hop snap restore a1b2c3d4 --restore-path /tmp/restore
```

## Repository initialization

The restic repository is initialized automatically on the first snapshot. If you need to initialize manually:

```bash
# Via the agent RPC (happens automatically)
hop snap create
```

## Snapshots and migration

Snapshots are the foundation of `hop to` migration. When you migrate to a new host, Hopbox:

1. Creates a snapshot on the current host
2. Bootstraps the new host
3. Restores the snapshot on the new host

See the [migration guide](./migration.md) for the full workflow.
