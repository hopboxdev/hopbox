---
sidebar_position: 2
---

# WireGuard Tunnel

The WireGuard tunnel is the core transport layer. It creates a private L3 network between your developer machine and VPS.

## Network layout

| | IP | Role |
|---|---|---|
| Client | `10.10.0.1/24` | Developer machine |
| Server | `10.10.0.2/24` | VPS (agent) |
| UDP port | `51820` | WireGuard transport |
| API port | `4200` | Agent control API (over tunnel) |
| MTU | `1420` | Default WireGuard MTU |
| Keepalive | `25s` | Client-side persistent keepalive |

## Client tunnel modes

### Kernel TUN (default)

On macOS, the client uses a kernel-level `utun` device created by the helper daemon. On Linux, it uses a `hopbox0` TUN device. Kernel TUN provides system-wide connectivity — any process can reach `10.10.0.2` without special configuration.

The helper daemon creates the TUN file descriptor and passes it to the unprivileged `hop` process. IP addresses and routes are configured by the helper.

### Netstack (for `hop to` only)

During `hop to` migration, a temporary userspace tunnel is created using gVisor's netstack. This avoids routing conflicts with the active kernel tunnel to the source host.

Netstack tunnels require explicit `DialContext()` for network operations — they are not system-wide. The netstack tunnel is automatically torn down after the migration completes.

## Server tunnel modes

### Kernel TUN (preferred)

The agent creates a kernel TUN device named `wg0` on the server. This requires `CAP_NET_ADMIN` capability, which the systemd service provides.

### Netstack fallback

If kernel TUN is unavailable (e.g., in a container without capabilities), the agent falls back to a userspace netstack tunnel. This is transparent to the client.

## Key exchange

WireGuard uses Curve25519 keypairs. During `hop setup`:

1. The client generates a keypair locally
2. The agent generates a keypair on the server (`hop-agent setup`)
3. Public keys are exchanged over the SSH session
4. Both sides configure WireGuard with the peer's public key

After the initial exchange, all communication happens over WireGuard. SSH is not used again for normal operations.

## Key rotation

Rotate keys without full re-setup:

```bash
hop rotate [host]
```

This generates new keypairs on both sides, exchanges them, and restarts the agent. The tunnel is briefly interrupted during rotation. The agent keeps a backup of the previous key at `agent.key.bak` for manual recovery.

## Reconnection resilience

A connection monitor (`ConnMonitor`) runs a 5-second heartbeat against the agent's `/health` endpoint.

**Monitoring behavior:**

| Parameter | Default |
|-----------|---------|
| Check interval | 5 seconds |
| Check timeout | 3 seconds |
| Failure threshold | 2 consecutive failures |

When the agent becomes unreachable after 2+ consecutive failed checks:

```
[14:32:05] Agent unreachable — waiting for reconnection...
```

When connectivity returns:

```
[14:32:47] Agent reconnected (was down for 42s)
```

WireGuard handles tunnel re-establishment natively — the monitor only observes and reports. No manual intervention is needed.

The monitor writes state to `~/.config/hopbox/tunnels/<host>.json`, which `hop status` reads to show `CONNECTED` and `LAST HEALTHY` fields.

## IPC configuration

WireGuard is configured through the `device.IpcSet()` interface (wireguard-go's key=value text protocol). The client and server each build their IPC configuration from the shared `tunnel.Config` struct:

- **Private key** — Curve25519 private key (hex-encoded)
- **Peer public key** — peer's Curve25519 public key
- **Endpoint** — `host:port` (client-side only, points to VPS public IP)
- **Allowed IPs** — subnet routed through the tunnel
- **Persistent keepalive** — 25 seconds (client-side only)
