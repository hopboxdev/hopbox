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

Building the golden rootfs (F6 will productize this; the F1.3 seed):

```sh
cp base.ext4 agent.ext4
truncate -s +96M agent.ext4 && e2fsck -fy agent.ext4; resize2fs agent.ext4
mnt=$(mktemp -d); mount -o loop agent.ext4 "$mnt"
install -m0755 hopbox-agent        "$mnt/usr/local/bin/hopbox-agent"
install -m0755 assets/hopbox-init  "$mnt/sbin/hopbox-init"
umount "$mnt"
```

## Running boxd on microVMs

```sh
sudo boxd --compute microvm \
  --fc-kernel /opt/hopbox-microvm/vmlinux \
  --fc-rootfs /opt/hopbox-microvm/agent.ext4 \
  --fc-rundir /var/lib/hopbox/microvm
# then: ssh box@<host>  ->  a Firecracker microVM
```

boxd derives the agent + metadata addresses from the VM gateway (`10.0.0.1`)
automatically; it needs root (KVM, tap, iptables). The egress fence on the VM
subnet is a follow-up.

Verified end to end on the KVM host (F1.5): `ssh box@host` spawns a microVM, the
agent connects, and a PTY shell bridges in —
`root@box:/#  MARKER=box__PID1=hopbox-agent__Ubuntu 22.04.5 LTS__5.10.223`.
