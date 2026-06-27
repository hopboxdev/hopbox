# microvm provider — golden rootfs

The provider boots each box from a **golden rootfs** (`New(..., rootfs, ...)`)
that must contain two things:

- `/usr/local/bin/hopbox-agent` — the agent (same binary as every box).
- `/sbin/hopbox-init` (`assets/hopbox-init`) — the in-VM init: it mounts the
  essentials and `exec`s the agent. The provider boots with `init=/sbin/hopbox-init`.

The agent's env (`HOPBOX_AGENT_TOKEN`, `HOPBOX_CONTROL_ADDR`, …) is injected via
the **kernel cmdline**: the kernel hands unrecognized `KEY=value` tokens to
init's environment, which the agent inherits. No DHCP, config-drive, or API
socket needed. Networking is the kernel `ip=` param (static eth0) on a host tap.

Building the golden rootfs — `build/microvm/build-rootfs.sh` (or `make
microvm-rootfs`). It cross-compiles `hopbox-agent` + `box-guest`, fetches a
pinned base ext4 + kernel, injects the binaries and `assets/hopbox-init`, and
emits `$OUT_DIR/{vmlinux, agent.ext4}`. Run on Linux as root (it loop-mounts the
image); supply prebuilt linux binaries via `$AGENT_BIN`/`$GUEST_BIN` when `go`
isn't on the build host.

```sh
sudo IMAGE=ubuntu-22.04 build/microvm/build-rootfs.sh   # build one catalog image
sudo AGENT_BIN=./agent GUEST_BIN=./box-guest \
     build/microvm/build-rootfs.sh                 # from prebuilt binaries
```

## Running boxd on microVMs

```sh
sudo boxd --compute microvm \
  --fc-kernel /opt/hopbox-microvm/vmlinux \
  --fc-images-dir /opt/hopbox-microvm/images --default-image ubuntu-22.04 \
  --fc-rundir /var/lib/hopbox/microvm
# then: ssh box@<host>  ->  a Firecracker microVM
```

boxd derives the agent + metadata addresses from the VM gateway (`10.0.0.1`)
automatically; it needs root (KVM, tap, iptables). Boxes are egress-fenced: they
may reach the agent hub + metadata ports and the public internet, but not the
host's other services, the LAN, or the tailnet.

Verified end to end on the KVM host (F1.5): `ssh box@host` spawns a microVM, the
agent connects, and a PTY shell bridges in —
`root@box:/#  MARKER=box__PID1=hopbox-agent__Ubuntu 22.04.5 LTS__5.10.223`.
