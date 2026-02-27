---
sidebar_position: 1
---

# CLI Reference

## Global flags

| Flag | Description |
|------|-------------|
| `-H, --host <name>` | Override which host to use for this command |
| `-v, --verbose` | Enable verbose output |

### Host resolution order

When a command needs a host, Hopbox resolves it in this order:

1. `--host` / `-H` flag
2. `host:` field in `./hopbox.yaml`
3. `default_host` in `~/.config/hopbox/config.yaml`
4. Error — you must specify one

---

## hop setup

Bootstrap a Linux VPS for use with Hopbox.

```bash
hop setup <name> -a <ip> [-u user] [-k keyfile] [-p port]
```

| Flag | Default | Description |
|------|---------|-------------|
| `<name>` | *(required)* | Host identifier |
| `-a, --addr` | *(required)* | SSH host IP or hostname |
| `-u, --user` | `root` | SSH username |
| `-k, --ssh-key` | *(auto)* | Path to SSH private key |
| `-p, --port` | `22` | SSH port |

Installs the agent, exchanges WireGuard keys, saves host config to `~/.config/hopbox/hosts/<name>.yaml`, and sets the host as default if none exists.

```bash
hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/id_ed25519
```

---

## hop up

Bring up the WireGuard tunnel, sync workspace, and start bridges.

```bash
hop up [workspace] [-f]
```

| Flag | Default | Description |
|------|---------|-------------|
| `[workspace]` | `./hopbox.yaml` | Path to manifest file |
| `-f, --foreground` | `false` | Run in foreground instead of daemon mode |

```bash
# Daemon mode (default)
hop up

# Foreground mode
hop up -f

# Specific manifest
hop up ./path/to/hopbox.yaml
```

---

## hop down

Tear down the tunnel and stop all bridges.

```bash
hop down
```

---

## hop status

Show host configuration, tunnel state, and agent health.

```bash
hop status
```

Displays: host name, agent endpoint, WireGuard IPs, connection status (`CONNECTED`), last health check (`LAST HEALTHY`), and forwarded ports.

---

## hop code

Open VS Code connected to the remote workspace.

```bash
hop code [path]
```

| Flag | Default | Description |
|------|---------|-------------|
| `[path]` | workspace path from manifest | Remote directory to open |

---

## hop run

Execute a named script from `hopbox.yaml`.

```bash
hop run <script>
```

```bash
hop run build
hop run test
```

Scripts are defined in the `scripts:` section of `hopbox.yaml`.

---

## hop services

Manage workspace services.

```bash
hop services [ls|restart|stop] [service]
```

| Subcommand | Description |
|------------|-------------|
| `ls` | List services with status |
| `restart <name>` | Restart a service |
| `stop <name>` | Stop a service |

```bash
hop services ls
hop services restart postgres
hop services stop api
```

Only shows services declared in `hopbox.yaml`.

---

## hop logs

Stream service logs.

```bash
hop logs [service]
```

Without a service name, streams logs from all services. With a name, streams logs from that service only.

```bash
hop logs
hop logs postgres
```

---

## hop snap

Manage workspace snapshots (restic-based).

```bash
hop snap [create|restore|ls]
```

| Subcommand | Description |
|------------|-------------|
| `create` | Create a new snapshot |
| `ls` | List available snapshots |
| `restore <id>` | Restore a snapshot by ID |

```bash
hop snap create
hop snap ls
hop snap restore a1b2c3d4
hop snap restore a1b2c3d4 --restore-path /tmp/restore
```

---

## hop to

Migrate workspace to a new host.

```bash
hop to <target> -a <ip> [-u user] [-k keyfile] [-p port]
```

Flags are the same as `hop setup`. Creates a snapshot on the current host, bootstraps the target, restores the snapshot, and switches the default host.

```bash
hop to newbox -a 5.6.7.8 -u debian -k ~/.ssh/key
```

---

## hop bridge

Manage local-remote bridges.

```bash
hop bridge [ls|restart]
```

| Subcommand | Description |
|------------|-------------|
| `ls` | List active bridges |
| `restart` | Restart all bridges |

---

## hop host

Manage the host registry.

```bash
hop host [add|rm|ls|default]
```

| Subcommand | Description |
|------------|-------------|
| `ls` | List registered hosts |
| `default` | Show the current default host |
| `default <name>` | Set the default host |
| `rm <name>` | Remove a host from the registry |

```bash
hop host ls
hop host default mybox
hop host rm oldbox
```

---

## hop upgrade

Upgrade hop binaries (client, helper, and agent).

```bash
hop upgrade [--version V] [--local]
```

| Flag | Description |
|------|-------------|
| `--version V` | Install a specific version |
| `--local` | Install from locally built binaries in `dist/` |

```bash
hop upgrade
hop upgrade --version 0.4.0
hop upgrade --local
```

---

## hop rotate

Rotate WireGuard keys without full re-setup.

```bash
hop rotate [host]
```

Generates new keypairs and exchanges them with the agent. The tunnel is briefly interrupted during the rotation.

---

## hop init

Generate a `hopbox.yaml` scaffold in the current directory.

```bash
hop init
```

---

## hop version

Print version and build information.

```bash
hop version
```
