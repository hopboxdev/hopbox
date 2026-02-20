# Kernel TUN + `hop code` Design

**Date:** 2026-02-20
**Status:** Approved

## Problem

Hopbox uses netstack (userspace WireGuard). The WireGuard IP `10.10.0.2` only
exists inside the `hop up` process — VS Code, `psql`, `curl`, and every other
tool on the developer's Mac can't reach it. This makes remote development feel
disconnected rather than local.

## Decision

Switch from netstack to kernel TUN mode via a privileged helper daemon. Add a
`hop code` command that opens VS Code Remote SSH to the workspace. Remove
`hop shell` (replaced by `ssh <name>.hop`).

## Design

### 1. Privileged Helper Daemon

A small LaunchDaemon (`dev.hopbox.helper`) installed once during `hop setup`.
Requires one-time `sudo`. Communicates with `hop up` via Unix socket at
`/var/run/hopbox/helper.sock`.

Two responsibilities:

1. **Create/destroy `utun` interfaces** — Creates a macOS `utun` device, assigns
   `10.10.0.1/24`, adds route to `10.10.0.2`.
2. **Manage `/etc/hosts` entries** — Adds `10.10.0.2 <name>.hop` on tunnel up,
   removes it on tunnel down.

The `hop up` process still owns the WireGuard protocol (keys, encryption, peers).
It delegates only the privileged TUN and hosts operations to the helper.

### 2. Changes to `hop setup`

After installing the agent and exchanging WireGuard keys:

1. Check if the privileged helper is installed.
2. If not, prompt: "Hopbox needs to install a system helper for tunnel
   networking. This requires sudo. Proceed? [y/n]"
3. Install helper binary to `/Library/PrivilegedHelperTools/dev.hopbox.helper`.
4. Install plist to `/Library/LaunchDaemons/dev.hopbox.helper.plist`.
5. Start the helper.

No fallback to netstack — if the helper isn't installed, `hop up` errors and
tells the user to run `hop setup`.

### 3. Changes to `hop up`

Current flow: create netstack device in-process, start WireGuard, start local
proxy for other commands.

New flow:

1. Connect to helper via Unix socket.
2. Send `create_tun` — helper creates `utun`, assigns IPs, adds route.
3. Start WireGuard protocol on the real TUN file descriptor.
4. Send `add_host` — helper writes `10.10.0.2 <name>.hop` to `/etc/hosts`.
5. Start bridges, services as before.
6. On shutdown (Ctrl-C / `hop down`): send `remove_host` + `destroy_tun`.

### 4. `hop code` Command

Add-on command. Requires `hop up` to be running.

```
hop code [workspace-path]
```

1. Check tunnel is up (read tunnel state file). Error if not.
2. Resolve host name (e.g., `mybox.hop`).
3. Write/update a managed block in `~/.ssh/config`:
   ```
   # --- hopbox managed ---
   Host mybox.hop
     HostName mybox.hop
     User gandalf
   # --- end hopbox ---
   ```
4. Launch: `code --remote ssh-remote+mybox.hop <workspace-path>`

Workspace path resolution (priority order):

1. CLI argument: `hop code /home/gandalf/my-project`
2. `editor.path` in `hopbox.yaml`
3. User's home directory on the VPS

### 5. `hopbox.yaml` Addition

```yaml
editor:
  type: vscode-remote
  path: /home/gandalf/my-project
  extensions:
    - golang.go
```

Extension auto-install is deferred — VS Code handles this via settings sync.

### 6. Codebase Simplification

Kernel TUN makes `10.10.0.2` a real system IP. This removes:

- The localhost proxy that `hop up` writes to tunnel state.
- `tun.DialContext` transport workaround for in-process netstack.
- The `CallVia` / `Call` distinction in `rpcclient` — everything uses `Call`
  with `<name>.hop:4200`.
- The `Ready()` channel on `ClientTunnel` (no more netstack init race).

All commands that hit the agent API use a plain `http.Client` targeting
`<name>.hop:4200`.

### 7. `hop shell` Removal

With kernel TUN, `ssh <name>.hop` is trivial. `hop shell` added zellij/tmux
session attachment, but with VS Code Remote SSH as the primary workflow, this
is low value. Removed to keep the command set lean.

### 8. Final Command Set

```
hop setup <name> -a <ip> [-u user] [-k keyfile]
hop up [workspace]
hop down
hop code [path]
hop status
hop run <script>
hop services [ls|restart|stop]
hop logs [service]
hop snap [create|restore|ls]
hop to <newhost>
hop bridge [ls|restart]
hop host [add|rm|ls|default]
hop rotate [host]
hop upgrade [host]
hop init
hop version
```

### 9. Host Naming

Hosts are reachable as `<name>.hop` where `<name>` comes from `hop setup <name>`.
Examples: `mybox.hop`, `gaming.hop`. Managed via `/etc/hosts` by the privileged
helper.
