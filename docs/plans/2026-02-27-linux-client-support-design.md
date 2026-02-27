# Linux Client Support Design

**Goal:** Enable `hop up` (kernel TUN tunnel) on Linux client machines by creating Linux-specific helper daemon installation, TUN device creation, and client tunnel files — mirroring the existing macOS architecture.

**Approach:** Mirror the macOS architecture. Create Linux-specific files that parallel the darwin ones. The helper protocol, socket path, hosts management, and client code are already platform-agnostic and need zero changes.

**Scope:** Native Linux desktops/laptops with systemd. WSL2 with systemd enabled also works. WSL1 and WSL2-without-systemd are out of scope (can be added later if there's demand).

---

## Helper Installation (`internal/helper/install_linux.go`)

Mirrors `install_darwin.go`. The helper binary is copied to `/usr/local/bin/hop-helper` and a systemd unit file is written to `/etc/systemd/system/hopbox-helper.service`:

```ini
[Unit]
Description=Hopbox privileged helper daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hop-helper
Restart=always

[Install]
WantedBy=multi-user.target
```

`Install()` runs `systemctl daemon-reload && systemctl enable --now hopbox-helper`.

`IsInstalled()` checks if the unit file exists (same pattern as macOS checking for the plist).

---

## TUN Device Creation (`internal/helper/tun_linux.go`)

Uses wireguard-go's `tun.CreateTUN("hopbox%d", mtu)` which opens `/dev/net/tun` and creates a kernel TUN interface. Returns the fd as `*os.File`.

IP and route configuration shells out to the `ip` command:

| Operation | macOS | Linux |
|-----------|-------|-------|
| Assign IP | `ifconfig utun5 inet 10.10.0.1 10.10.0.2 netmask 255.255.255.0 up` | `ip addr add 10.10.0.1/24 dev hopbox0` + `ip link set hopbox0 up` |
| Add route | `route -n add -net 10.10.0.0/24 -interface utun5` | `ip route add 10.10.0.0/24 dev hopbox0` |
| Delete route | `route -n delete -net 10.10.0.0/24` | `ip route del 10.10.0.0/24 dev hopbox0` |
| Cleanup | fd close destroys utun | `ip link delete hopbox0` (TUN persists unless explicitly deleted) |

`SetMTU` uses ioctl (same as macOS) or `ip link set hopbox0 mtu 1420`.

---

## Client Tunnel (`internal/tunnel/client_linux.go`)

Nearly identical to `client_darwin.go`. Same `KernelTunnel` struct with `Start`/`Stop`/`Ready`/`Status` methods.

Flow:
1. Receive fd + interface name from helper via socket
2. `tun.CreateTUNFromFile(fd, 0)` to wrap for wireguard-go
3. `device.NewDevice()` + `IpcSet()` + `Up()`
4. Block until context cancelled, then close

Interface naming: macOS uses `utun5` (kernel-assigned), Linux uses `hopbox0` (from `tun.CreateTUN("hopbox%d", mtu)`). The name is passed as a string; nothing depends on the prefix.

---

## Helper Daemon Changes (`cmd/hop-helper/`)

The daemon (`main.go`) is already platform-agnostic except for `handleCreateTUN`. Extract it into OS-specific files:

- `handlecreatetun_darwin.go` — existing macOS logic (AF_SYSTEM utun, `GetsockoptString` for interface name)
- `handlecreatetun_linux.go` — calls `helper.CreateTUNDevice(mtu)`, extracts fd, sends via SCM_RIGHTS

Everything else in `main.go` stays shared: socket listening, JSON protocol, signal handling, `/etc/hosts` management, all other action handlers.

---

## Unchanged Files

| File | Why unchanged |
|------|---------------|
| `internal/helper/client.go` | Socket communication is platform-agnostic |
| `internal/helper/protocol.go` | Request/response types are shared |
| `internal/helper/hosts.go` | `/etc/hosts` management is pure file I/O |
| `cmd/hop/up.go` | Talks to helper via socket, no OS-specific code |
| `cmd/hop/setup.go` | Helper install prompt already works generically |
| `internal/tunnel/client.go` | Netstack `ClientTunnel` (for `hop to`) unchanged |

---

## Files Summary

**New files:**

| File | Purpose |
|------|---------|
| `internal/helper/install_linux.go` | systemd unit install/uninstall, `IsInstalled()` |
| `internal/helper/tun_linux.go` | `CreateTUNDevice()`, `SetMTU()`, `ConfigureTUN()`, `CleanupTUN()` |
| `internal/tunnel/client_linux.go` | `KernelTunnel` struct (mirrors darwin version) |
| `cmd/hop-helper/handlecreatetun_darwin.go` | Extract existing `handleCreateTUN` (darwin build tag) |
| `cmd/hop-helper/handlecreatetun_linux.go` | Linux `handleCreateTUN` |

**Modified files:**

| File | Change |
|------|--------|
| `cmd/hop-helper/main.go` | Remove `handleCreateTUN` (moved to OS-specific files) |
| `ROADMAP.md` | Check off Linux client support |

---

## Tech Choices

- **Init system:** systemd only (covers Ubuntu, Debian, Fedora, Arch)
- **TUN creation:** wireguard-go `tun.CreateTUN` (proven on server side)
- **IP/route config:** shell out to `ip` command (consistent with macOS shelling out to `ifconfig`/`route`)
- **WSL:** WSL2 with systemd works; WSL1 and WSL2-without-systemd deferred
