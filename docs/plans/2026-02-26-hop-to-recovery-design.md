# `hop to` Error Recovery Design

**Goal:** When `hop to` fails at any phase, clean up client-side state and exit
with a clear error message so the user can re-run from scratch.

**Strategy:** Idempotent retry with client-side cleanup. No resume, no migration
state file, no server-side rollback.

## Why This Works

Each phase is idempotent on retry:

- **Phase 1 (snapshot):** Creates a new snapshot. Old snapshots are harmless.
- **Phase 2 (bootstrap):** `hop setup` overwrites binary, keys, restarts systemd.
- **Phase 3 (restore):** `restic restore` overwrites files with snapshot contents.

## Changes to `to.go`

### Client-side cleanup on failure

Track whether bootstrap saved a host config (`targetConfigSaved bool`). On any
error after bootstrap, delete `~/.config/hopbox/hosts/<target>.yaml` so the
client doesn't point at a half-configured host.

Default host is already set as the very last step, so it's never updated if
anything fails.

### Error messages per phase

Each failure prints which phase failed, what state exists, and a re-run hint:

- **Phase 1 fails:** "Migration failed during snapshot creation. No changes were
  made. Re-run: `hop to <target> ...`"
- **Phase 2 fails:** "Migration failed during target bootstrap. Partial agent
  install may exist on `<target>` and will be overwritten on retry. Re-run:
  `hop to <target> ...`"
- **Phase 3 fails:** "Migration failed during restore. Snapshot `<id>` exists
  and target `<target>` is bootstrapped. Re-run: `hop to <target> ...`"

Messages are factual: what happened, what state remains, how to retry.

## What Doesn't Change

- TUI runner phase/step structure stays the same.
- Snapshot, bootstrap, and restore logic are unchanged.
- No new files or abstractions. All changes are in `cmd/hop/to.go`.

## Scope

- Add `targetConfigSaved` tracking and defer-based cleanup to `to.go`.
- Improve error messages at each phase boundary.
- Update ROADMAP.md to mark `hop to` error recovery as complete.
