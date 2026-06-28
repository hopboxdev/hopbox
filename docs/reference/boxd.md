# `boxd` configuration

`boxd` is the standalone **compute-box daemon**: `ssh box@host` spawns a box (a
Firecracker microVM or a Docker container) and bridges your session into it. It
is the box core with **no dev-env compiled in** — no gRPC API, no CLI, no
gateway. All settings are flags. Run `boxd --help` for the authoritative list.

For the concepts behind these flags, see [boxd — compute over SSH](/guide/boxd).
The one-command installer (`deploy/install-boxd.sh`) maps each flag to a
`HOPBOX_*` variable in `/etc/hopbox/boxd.env`.

## Front door (SSH)

The SSH listener where the **username is a box spec** and the **client key is the
identity** — no signup. See the [username grammar](/guide/ssh#username-grammar).

| Flag | Default | Description |
| --- | --- | --- |
| `--ssh-addr` | `:2222` | Front-door SSH listen address (username = box spec, key = identity). |
| `--host-key` | `./boxd-ssh-host-key` | Front-door SSH host key path (auto-created on first run). |
| `--default-image` | `alpine` | Image used when the username names none. With the microVM backend set this to a catalog image (e.g. `ubuntu-22.04`). |
| `--default-cpus` | `2` | CPU cap (vCPU) per box. `0` = unlimited. A named flavor in the spec overrides this. |
| `--default-mem-mb` | `2048` | Memory cap (MB) per box. `0` = unlimited. |

## Lifecycle (ephemeral vs persistent)

Boxes are **ephemeral by default**: reaped a short `--grace` after you
disconnect. `--auto-suspend` flips the daemon to the **persistent tier** —
boxes suspend to disk when idle and resume instantly on reconnect. See
[ephemeral vs persistent](/guide/boxd#ephemeral-vs-persistent).

| Flag | Default | Description |
| --- | --- | --- |
| `--grace` | `2m` | Ephemeral reconnect window: keep a box this long after disconnect before reaping. `0` = reap immediately. A reconnect within the window cancels the reap. |
| `--auto-suspend` | `false` | Persistent tier: boxes auto-suspend when idle and wake on reconnect instead of being reaped. |
| `--idle-timeout` | `5m` | Suspend a box after this long idle (with `--auto-suspend`). |

## Compute backend

| Flag | Default | Description |
| --- | --- | --- |
| `--compute` | `docker` | Compute backend: `docker` \| `microvm`. |
| `--agent-bin` | _(empty)_ | Docker: host path of the Linux `hopbox-agent` binary side-loaded into each box. (The microVM backend bakes the agent into the rootfs.) |
| `--guest-bin` | _(empty)_ | Docker: host path of the Linux `box-guest` binary side-loaded into each box. (The microVM backend bakes it into the rootfs.) |

### microVM (Firecracker)

Active when `--compute microvm`. A box is a Firecracker microVM booted from a
CoW clone of a catalog image. See
[microVM & the image catalog](/guide/boxd#microvm-image-catalog).

| Flag | Default | Description |
| --- | --- | --- |
| `--fc-bin` | `/usr/local/bin/firecracker` | Firecracker binary. |
| `--fc-kernel` | `/opt/hopbox-microvm/vmlinux` | `vmlinux` guest kernel. |
| `--fc-images-dir` | `/opt/hopbox-microvm/images` | Base-image catalog dir; image `<name>` resolves to `<dir>/<name>.ext4`. Built with `build/microvm/build-rootfs.sh` / `build-deboot.sh`. |
| `--fc-rundir` | `/var/lib/hopbox/microvm` | Per-VM working dir (CoW disks, sockets, durable home images). |
| `--fc-bridge` | _(empty → `hopbox-vmnet`)_ | Host bridge for the microVM fleet. Set with `--fc-subnet` to run a second fleet beside another daemon. |
| `--fc-subnet` | _(empty → `10.0.0`)_ | `/24` base — first three octets. The bridge gateway is `.1`; boxes reach the host (agent hub + metadata) there. |

## Agent hub & metadata

| Flag | Default | Description |
| --- | --- | --- |
| `--agent-listen` | `:7777` | Address the in-box agent dials back on (reverse tunnel). |
| `--advertise` | _(empty)_ | Address the in-box agent is told to dial. Empty = derived from the backend gateway (`host.docker.internal` for docker, the bridge `.1` for microVM) + the `--agent-listen` port. |
| `--meta-addr` | `:8090` | Box metadata API listen address. Boxes reach it by source IP — this is what powers [`box-guest`](/guide/boxd#box-guest-mcp) (`info` / `keep-alive` / `auto-suspend` / `idle`). |
| `--db` | `./boxd.db` | Box database path (SQLite). Tracks box ownership, state, and durable-home mapping across restarts. |

## See also

- [boxd — compute over SSH](/guide/boxd) — the concepts and a walkthrough.
- [SSH & the front door](/guide/ssh) — the username grammar and the image catalog.
- [`hopboxd` config](/reference/hopboxd) — the dev-env control plane built on the same box core.
