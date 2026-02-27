---
sidebar_position: 2
---

# Workspace Lifecycle

This guide covers the day-to-day workflow: bringing up the tunnel, syncing your workspace, monitoring connectivity, and tearing it down.

## Bringing up the tunnel

```bash
hop up
```

By default, `hop up` starts a background daemon. You'll see a brief status output:

```
Bringing up tunnel to mybox (1.2.3.4)
Interface utun7 up, 10.10.0.1 → mybox.hop
Agent probe... healthy
Agent is up.
```

If a `hopbox.yaml` exists in the current directory, the manifest is synced to the agent, which installs packages, starts services, and activates bridges.

### Foreground mode

Use `-f` to run in the foreground (useful for debugging):

```bash
hop up -f
```

In foreground mode, the tunnel runs until you press Ctrl-C.

### Workspace path

To use a `hopbox.yaml` from a specific location:

```bash
hop up ./path/to/hopbox.yaml
```

## Tearing down

```bash
hop down
```

This stops the daemon, tears down the WireGuard tunnel, removes the TUN device, and cleans up the `/etc/hosts` entry.

In foreground mode, Ctrl-C does the same thing.

## Checking status

```bash
hop status
```

The status dashboard shows:

- **Host name** and agent endpoint
- **WireGuard tunnel** IPs (`10.10.0.1` → `10.10.0.2`)
- **CONNECTED** — whether the agent is currently reachable
- **LAST HEALTHY** — timestamp of the last successful health check
- **Forwarded ports** — ports auto-forwarded from the server

## Host resolution

When you run `hop up` (or any command that needs a host), Hopbox resolves which host to use in this order:

1. `--host` / `-H` flag on the command line
2. `host:` field in `./hopbox.yaml`
3. `default_host` in `~/.config/hopbox/config.yaml`
4. Error — you must specify one of the above

## Reconnection resilience

The tunnel is designed to survive network interruptions. A connection monitor runs a 5-second heartbeat, checking the agent's health endpoint.

If the agent becomes unreachable for two consecutive checks:

```
[14:32:05] Agent unreachable — waiting for reconnection...
```

When connectivity returns:

```
[14:32:47] Agent reconnected (was down for 42s)
```

WireGuard handles tunnel re-establishment natively. The monitor only observes and reports — no manual intervention is needed.

The connection state is written to `~/.config/hopbox/tunnels/<host>.json`, which `hop status` reads.

## Version mismatch handling

If the agent and client versions differ, `hop up` prompts you to upgrade:

```
Agent version (0.3.1) differs from client (0.4.0).
Run 'hop upgrade' to update the agent.
```

You can upgrade with:

```bash
hop upgrade
```

## Environment variables

If `.env` or `.env.local` files exist next to `hopbox.yaml`, they are loaded automatically. Precedence (last wins):

1. `.env` — shared, committed to version control
2. `.env.local` — personal overrides, gitignored by convention
3. `env:` in `hopbox.yaml`
4. Service-level `env:` in `hopbox.yaml`

Services are always recreated on `hop up`, so environment changes take effect immediately.
