# Unified `hop upgrade` Design

## Goal

Replace the agent-only `hop upgrade` with a single command that updates all three
binaries: client CLI, helper daemon, and agent. Support both GitHub releases and
local dev builds.

## CLI Interface

```
hop upgrade                     # upgrade all components to latest release
hop upgrade --version v0.3.0    # upgrade to specific version
hop upgrade --local             # upgrade from ./dist/ (dev builds)
hop upgrade --client-only       # just the hop binary
hop upgrade --agent-only        # just the agent on the server
hop upgrade --helper-only       # just the helper daemon
```

No flags = all three components, in order: client, helper, agent.

## Component Update Flows

### Client Self-Update

1. Download new binary from GitHub releases (or copy from `./dist/hop` if `--local`).
2. Verify SHA256 checksum (releases only).
3. Detect package manager — if managed, print message and skip.
4. Write to `<current-path>.new`, chmod +x, then `os.Rename` atomically over self.
5. The running process continues fine (OS keeps old inode open).

### Helper Update (macOS only)

1. Download new `hop-helper` binary (or `./dist/hop-helper` if `--local`).
2. Write to temp file, chmod +x.
3. Run `sudo <temp>/hop-helper --install` — copies to
   `/Library/PrivilegedHelperTools/` and reloads the LaunchDaemon.
4. Existing `--install` logic handles everything.

### Agent Update

1. Same as current `hop upgrade` — download, checksum, SCP upload, systemd restart.
2. Resolves host the same way (flag, hopbox.yaml, default).

## Package Manager Detection

**Build-time flag:** `-X version.PackageManager=brew` via ldflags in the package
recipe.

**Runtime fallback:** Check `os.Executable()` path for `/Cellar/`, `/homebrew/`,
`/nix/store/`.

**Behavior when detected:** Print
`"hop was installed via <manager>. Run '<manager command>' instead."` — skip
client update, still offer helper + agent updates.

## Version Checking

Each component reports its version:

- **Client:** `version.Version` (in-process).
- **Helper:** New request over the Unix socket — returns helper version string.
- **Agent:** Existing SSH `hop-agent version` check.

Skip components already at target version (idempotent).

## `--local` Mode

Reads from `./dist/`:

- `./dist/hop` — client
- `./dist/hop-helper` — helper
- `./dist/hop-agent-linux` — agent

No checksum verification. Version displays as "dev". Useful during development
after `make build`.

## Output

```
$ hop upgrade

Checking for updates...
  Client:  v0.2.0 -> v0.3.0
  Helper:  v0.2.0 -> v0.3.0
  Agent:   v0.2.0 -> v0.3.0

Upgrading client... done (replaced /usr/local/bin/hop)
Upgrading helper... (requires sudo)
Password: ****
  done (updated LaunchDaemon)
Upgrading agent on mybox... done (restarted hop-agent)

All components upgraded to v0.3.0.
```

## Scope Exclusions

- Homebrew/Nix package publishing is a separate future effort.
- No auto-update or update-check on every command.
