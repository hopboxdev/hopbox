---
sidebar_position: 1
---

# Setup

The `hop setup` command bootstraps a Linux VPS for use with Hopbox. It installs the agent, exchanges WireGuard keys, and saves the host configuration locally.

## Command

```bash
hop setup <name> -a <ip> [-u user] [-k keyfile] [-p port]
```

| Flag | Default | Description |
|------|---------|-------------|
| `<name>` | *(required)* | Identifier for this host (used in configs and as `<name>.hop` hostname) |
| `-a, --addr` | *(required)* | SSH host IP or hostname |
| `-u, --user` | `root` | SSH username |
| `-k, --ssh-key` | *(auto-detected)* | Path to SSH private key |
| `-p, --port` | `22` | SSH port |

If no key is specified, Hopbox checks the SSH agent and then tries `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, and `~/.ssh/id_ecdsa` in order.

## What happens during setup

### 1. SSH connection (trust-on-first-use)

On the first connection, you'll be asked to confirm the server's SSH host key fingerprint:

```
Host key fingerprint: SHA256:abcdef1234567890...
Trust this host? (yes/no):
```

Type `yes` to continue. The host key is saved so future connections are verified automatically. The SSH connection retries up to 3 times with a 30-second timeout.

### 2. Agent installation

Hopbox detects the VPS architecture and downloads the correct `hop-agent` binary from GitHub releases. It verifies the SHA256 checksum, uploads the binary via SCP, and installs it as a systemd service.

The binary is written atomically (to a temporary file, then moved) to avoid "text file busy" errors when replacing a running agent.

To use a locally built agent binary instead of downloading:

```bash
HOP_AGENT_BINARY=./dist/hop-agent-linux hop setup mybox -a 1.2.3.4
```

### 3. WireGuard key exchange

Setup generates Curve25519 keypairs on both the client and the server, then exchanges public keys over the SSH session. After the exchange, the agent is restarted with `systemctl enable && systemctl restart hop-agent`.

### 4. Host config saved

The host configuration is written to:

```
~/.config/hopbox/hosts/<name>.yaml
```

This file contains the WireGuard keys, tunnel IPs, SSH endpoint, and SSH host key.

### 5. Default host

If no default host is configured, `hop setup` sets the new host as the default. You can change the default later:

```bash
hop host default mybox
```

### 6. Helper daemon installation

On the first setup, you'll be prompted to install the privileged helper daemon. This is required for `hop up` to work — the helper manages TUN device creation and `/etc/hosts` entries.

```
Install privileged helper? (requires sudo) [y/N]: y
```

The helper is installed once per machine. See the [helper daemon architecture](../architecture/helper-daemon.md) for details.

## Re-running setup

Running `hop setup` again for the same host name overwrites the existing configuration. This is useful for:

- Pointing a host at a new IP (after VPS migration)
- Regenerating WireGuard keys
- Upgrading the agent binary

For key-only rotation without full re-setup, use `hop rotate`.
