# `hopbox` CLI

The `hopbox` CLI talks to `hopboxd` over gRPC. Set the server with
`--addr` (default `localhost:7700`); on multi-user servers it sends the token
saved by `hopbox login`.

## Workspaces

| Command | Description |
| --- | --- |
| `hopbox create <name> --image <ref> [--expose name:port] [--mem MB]` | Create a workspace. |
| `hopbox ls` | List your workspaces. |
| `hopbox get <name\|id>` | Show a workspace and its resolved endpoints. |
| `hopbox rm <name\|id>` | Destroy a workspace. |

## Run things

| Command | Description |
| --- | --- |
| `hopbox shell <name\|id>` | Interactive PTY shell over the control plane. |
| `hopbox exec <name\|id> -- <cmd>…` | Run a command non-interactively. |

## SSH

| Command | Description |
| --- | --- |
| `hopbox login [--token <tok>]` | Authenticate and fetch a short-lived SSH certificate. `--token` for multi-user servers. |
| `hopbox ssh-config <name\|id> [--alias a] [--user u]` | Write an `~/.ssh` entry so `ssh <name>` / VS Code work. |
| `hopbox ssh <name\|id> [-- ssh args…]` | Connect via the system `ssh` (no config needed). |
| `hopbox proxy <name\|id>` | Stdio SSH transport — used internally as an OpenSSH `ProxyCommand`. |

See [SSH & VS Code](/guide/ssh) and [Auth & multi-user](/guide/auth).

## Global flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `localhost:7700` | `hopboxd` API address. |
