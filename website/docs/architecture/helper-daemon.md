---
sidebar_position: 3
---

# Helper Daemon

The helper daemon (`hop-helper`) is a privileged process that handles operations requiring root: creating TUN devices, configuring network interfaces, and managing `/etc/hosts` entries.

## Why a helper daemon

TUN device creation and IP/route configuration require root privileges. Rather than running the entire `hop` client as root, a small helper daemon runs with elevated privileges and communicates with the unprivileged client over a Unix socket.

On macOS, the helper runs as a LaunchDaemon. On Linux, it runs as a systemd service.

## Installation

The helper is installed automatically during `hop setup`. It can also be installed manually:

```bash
sudo hop-helper --install
```

| Platform | Binary path | Service definition |
|----------|------------|-------------------|
| macOS | `/Library/PrivilegedHelperTools/dev.hopbox.helper` | `/Library/LaunchDaemons/dev.hopbox.helper.plist` |
| Linux | `/usr/local/bin/hop-helper` | `/etc/systemd/system/hopbox-helper.service` |

## Communication protocol

The helper listens on a Unix domain socket at `/var/run/hopbox/helper.sock`. The client sends JSON requests and receives JSON responses.

**Request format:**

```json
{
  "action": "create_tun",
  "interface": "",
  "local_ip": "10.10.0.1",
  "peer_ip": "10.10.0.2",
  "mtu": 1420
}
```

**Response format:**

```json
{
  "ok": true,
  "interface": "utun7"
}
```

## Actions

### create_tun

Creates a TUN device and returns its name. On macOS, the file descriptor is passed back to the client via SCM_RIGHTS (fd passing over the Unix socket).

**macOS:** Creates a `utun` device using an `AF_SYSTEM` socket with the `com.apple.net.utun_control` control. The unit number is auto-assigned. The device is destroyed when the file descriptor is closed.

**Linux:** Creates a TUN device named `hopbox0` (or `hopbox1`, etc.) using wireguard-go's `tun.CreateTUN()`. Linux TUN devices persist until explicitly deleted.

### configure_tun

Assigns an IP address and configures routing for the TUN device.

**macOS:**
```
ifconfig utun7 inet 10.10.0.1 10.10.0.2 netmask 255.255.255.0 up
route -n add -net 10.10.0.0/24 -interface utun7
```

**Linux:**
```
ip addr add 10.10.0.1/24 dev hopbox0
ip link set hopbox0 up
ip route add 10.10.0.0/24 dev hopbox0
```

MTU is configured separately via `SIOCSIFMTU` ioctl (macOS) or `ip link set` (Linux).

### cleanup_tun

Removes routes and, on Linux, deletes the TUN device.

**macOS:** Removes the route; the utun device is destroyed when its fd is closed.

**Linux:** Removes the route and deletes the device with `ip link delete`.

### add_host

Adds a `<name>.hop` entry to `/etc/hosts`:

```
10.10.0.2    mybox.hop
```

This makes the agent reachable by hostname from any process.

### remove_host

Removes the `<name>.hop` entry from `/etc/hosts`.

### version

Returns the helper daemon's version string.

## Security model

The helper daemon:

- Runs as root (required for network configuration)
- Communicates only via Unix socket (not over the network)
- Validates all requests before executing
- Only performs the specific actions listed above — it does not provide general root access
