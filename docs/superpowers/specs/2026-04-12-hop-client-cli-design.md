# hop — Hopbox Client CLI

## Overview

A client-side CLI that runs on the user's machine and wraps ssh/scp to provide ergonomic access to Hopbox dev environments. Separate from the in-container `hopbox` binary.

## Commands

### `hop` (no subcommand)

If a default box is configured, opens an interactive SSH session to it. Otherwise, prints help.

### `hop init`

Interactive setup wizard. Prompts for:
- Server hostname (e.g., `hopbox.dev`)
- SSH port (default: `2222`)
- Username
- Default box name

Writes config to `~/.config/hop/config.toml`.

### `hop ssh [--box NAME]`

Opens an interactive SSH session. Uses `--box` flag or falls back to `default_box` from config.

Executes: `ssh -p <port> <user>@<server>` (no box specified — gateway shows the picker if multiple boxes exist).

With `--box`: `ssh -p <port> <user>+<box>@<server>` (gateway routes directly, no picker).

### `hop expose PORT`

Opens an SSH tunnel in the foreground. Ctrl-C to stop.

Executes: `ssh -p <port> -L <PORT>:localhost:<PORT> -N <user>+<box>@<server>`

Prints a message like: `Forwarding localhost:<PORT> -> box:<PORT> (ctrl-c to stop)`

### `hop transfer FILE [:[REMOTE_PATH]]`

Uploads a file to the box. Default remote path is `~/`.

Executes: `scp -O -P <port> <FILE> <user>+<box>@<server>:<REMOTE_PATH>`

The `-O` flag forces legacy SCP protocol since the Hopbox gateway doesn't implement SFTP subsystem.

Examples:
- `hop transfer ./file.txt` -> uploads to `~/file.txt`
- `hop transfer ./file.txt:/home/dev/projects/` -> uploads to specified path

### `hop config`

Prints the fully resolved configuration (after flag/env/file merge). Useful for debugging direnv setups.

Output example:
```
server:      hopbox.dev
port:        2222
user:        gandalf
default_box: main
source:      /Users/gandalf/.config/hop/config.toml (overrides: HOP_BOX)
```

## Configuration

### Config file

Path: `~/.config/hop/config.toml`

```toml
server = "hopbox.dev"
port = 2222
user = "gandalf"
default_box = "main"
```

### Environment variables

| Variable | Overrides |
|---|---|
| `HOP_SERVER` | `server` |
| `HOP_PORT` | `port` |
| `HOP_USER` | `user` |
| `HOP_BOX` | `default_box` |

### Precedence

Flags > environment variables > config file.

## Implementation

- Language: Go (same repo, `cmd/hop/main.go`)
- CLI framework: `kong` (already used by the in-container `hopbox` CLI)
- SSH execution: `os/exec` shelling out to the user's `ssh` and `scp` binaries
- Config parsing: `pelletier/go-toml/v2` (already a dependency)
- No new dependencies required

### Why shell out to ssh/scp

The user already has SSH keys, agent forwarding, and `~/.ssh/config` set up. Shelling out to their `ssh` binary inherits all of that for free. Reimplementing an SSH client in Go would duplicate config, break agent forwarding, and add complexity for no benefit.

## Out of scope

- Download from box (can be added later as `hop transfer :remote local`)
- Multi-server profiles
- Box creation/deletion from client side
- Shell completions
- Windows support (ssh/scp assumed available)
