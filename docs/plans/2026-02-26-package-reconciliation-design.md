# Package Reconciliation — Design

## Problem

When a package is removed from `hopbox.yaml`, the agent has no way to know it
should be uninstalled. Over time, workspaces accumulate stale binaries,
apt packages, and nix profiles that are no longer in the manifest.

## Decision

Integrate package reconciliation into `workspace.sync`. The agent becomes the
single authority for converging workspace state to match the manifest. This
follows the same pattern already used for services (stop old, start new) and
scales to future reconcilable resources.

## State File

Path: `/etc/hopbox/installed-packages.json`

```json
{
  "packages": [
    {"name": "htop", "backend": "apt"},
    {"name": "ripgrep", "backend": "static"},
    {"name": "nodejs", "backend": "nix", "version": "20"}
  ]
}
```

Written atomically (write to `.tmp`, rename) after every reconciliation.
The `internal/packages` package owns reading and writing this file.

## Reconcile Function

New public function: `packages.Reconcile(ctx context.Context, desired []Package) error`

1. Load state file → `previously` set
2. Build `desired` set from manifest packages
3. **Install**: packages in `desired` but not in `previously` (or changed version/URL)
4. **Remove**: packages in `previously` but not in `desired`
5. **Skip**: packages that are unchanged
6. Update state file with the new `desired` list

### Removal per backend

| Backend | Removal command |
|---------|----------------|
| apt     | `apt-get remove -y <name>` |
| nix     | `nix profile remove <name>` |
| static  | Delete `/opt/hopbox/bin/<binName>` + `/opt/hopbox/bin/.pkg/<name>` |

## Integration into workspace.sync

`Agent.Reload(ws)` gains a package reconciliation step before starting services
(services may depend on packages):

```
Reload(ws):
  1. packages.Reconcile(ctx, ws.Packages)   ← NEW
  2. BuildServiceManager(ws)
  3. InstallBridgeScripts(ws)
  4. Stop old services → Start new services
```

## Client Changes

Remove the separate `packages.install` TUI step from `up.go`. The "Syncing
manifest" step now covers packages. Rename it to "Syncing workspace."

The `packages.install` RPC endpoint stays as a lower-level API for ad-hoc use
but is no longer called during `hop up`.

## Error Handling

Package failures are logged but do not block the sync.

- If removing a package fails, the state file still lists it as installed
  (retried on next sync).
- If installing a new package fails, it is not added to the state file
  (retried on next sync).
- Services start regardless of package errors (same NonFatal behavior as today).

## Key Files

| File | Change |
|------|--------|
| `internal/packages/packages.go` | Add `Reconcile`, `Remove`, state file I/O |
| `internal/packages/state.go` | New file for state file types and helpers |
| `internal/agent/agent.go` | Call `packages.Reconcile` in `Reload` |
| `cmd/hop/up.go` | Remove `packages.install` TUI step |
