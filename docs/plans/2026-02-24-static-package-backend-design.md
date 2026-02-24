# Static Package Backend

## Context

Hopbox installs developer tools on the server via `apt` or `nix`. Many tools
(ripgrep, fd, lazygit, k9s, just, etc.) are distributed as standalone binaries
on GitHub releases rather than through package managers. The static backend lets
users declare these in `hopbox.yaml` and have the agent download + install them
automatically.

## Manifest Schema

New fields on `Package` (and `manifest.Package`):

```yaml
packages:
  - name: ripgrep
    backend: static
    url: https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz
    sha256: a1b2c3...   # optional
```

- `url` — required when `backend: static`, ignored for apt/nix
- `sha256` — optional hex-encoded SHA256; verified after download if present

Manifest parser validates that `url` is set when `backend: static`.

## Install Path

Binaries go to `/opt/hopbox/bin/`. This keeps hopbox-managed binaries separate
from system packages. No duplicate handling — if the user declared it, they
want it; PATH ordering decides precedence.

## PATH Setup

- **Agent process**: prepend `/opt/hopbox/bin` to `PATH` at startup in
  `cmd/hop-agent/main.go`. Covers `IsInstalled` checks and `run.script` execution.
- **User SSH sessions**: write `/etc/profile.d/hopbox.sh` with
  `export PATH="/opt/hopbox/bin:$PATH"` on first static install (idempotent).

## Installation Flow

`staticInstall(ctx, pkg)` in `internal/packages/packages.go`:

1. Download — HTTP GET to `pkg.URL`, write to temp file
2. Verify — if `pkg.SHA256` set, compute SHA256 and compare
3. Detect format — from URL extension: `.tar.gz`/`.tgz` -> tar+gzip,
   `.zip` -> zip, anything else -> raw binary
4. Extract — unpack to temp directory
5. Find binary — walk extracted files for an executable matching `pkg.Name`;
   fall back to the sole executable if only one exists
6. Install — move to `/opt/hopbox/bin/<name>`, chmod 0755
7. PATH file — ensure `/etc/profile.d/hopbox.sh` exists

`staticIsInstalled(pkg)`: check if `/opt/hopbox/bin/<name>` exists and is executable.

## Error Cases

- `url` missing -> validation error at manifest parse
- Download fails -> error with HTTP status
- SHA256 mismatch -> error with expected vs actual hashes
- No executable in archive -> error listing archive contents
- Multiple executables, none matching name -> error suggesting user check `name`

## Files to Modify

| File | Change |
|------|--------|
| `internal/manifest/manifest.go` | Add `URL`, `SHA256` fields to Package struct |
| `internal/packages/packages.go` | Implement `staticInstall`, `staticIsInstalled` |
| `cmd/hop-agent/main.go` | Prepend `/opt/hopbox/bin` to PATH at startup |
| `internal/packages/packages_test.go` | Tests for static backend |
| `internal/manifest/manifest_test.go` | Validation tests for url requirement |

## Approach

Inline implementation — add `staticInstall()` alongside existing `aptInstall()`
and `nixInstall()` in the same file. No interface refactor needed for 3 backends.
