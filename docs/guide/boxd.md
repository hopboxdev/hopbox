# boxd — compute over SSH

**boxd** is the standalone half of the project: a single daemon that turns one
host into a fleet of **compute boxes you reach over plain SSH**.

```sh
ssh box@box.hopbox.dev
```

That one command spawns a Firecracker microVM and drops you into a root shell.
No signup, no client to install, no pre-created box — your **SSH key is your
identity** and the **username is the box spec**. boxd is the *compute layer*;
[hopbox](/guide/what-is-hopbox) is a full dev-env platform built on the same
box core.

It is live at **box.hopbox.dev** — try the command above.

## Your key is your identity

There is no account to create. The front door accepts **any** SSH key and
derives the owner from its fingerprint:

- Reconnect with the same key → you get the **same box** back (within its
  lifetime).
- A different key asking for the same box name is refused while that box is
  alive — names are **per-owner**, so your `proj` and someone else's `proj` are
  distinct boxes.

```sh
ssh proj@box.hopbox.dev          # spawn/attach your box "proj"
ssh proj@box.hopbox.dev          # later — same key, same box
```

## The username is the spec

The SSH username is a small grammar that picks the box name, image, backend,
and lifetime:

```
name[~backend][:image[:flavor[+duration]]]
```

```sh
ssh proj@host                    # box "proj", default image
ssh proj:ubuntu-22.04@host       # pick a catalog image
ssh proj:debian-12:cpu+1h@host   # image + flavor, stay alive 1h after disconnect
ssh proj+@host                   # force a fresh box (trailing +)
ssh images@host                  # list the image catalog (spawns no box)
```

The full grammar is documented once in [SSH & the front door](/guide/ssh#username-grammar).

## Files in and out {#files}

The front door speaks the SFTP subsystem, so `scp` and `sftp` work against a box
like any SSH host. Paths are relative to the box home, so they land where a shell
does:

```sh
scp -r ./src proj@box.hopbox.dev:src      # copy a tree in
scp proj@box.hopbox.dev:out.tgz .         # copy results out
sftp proj@box.hopbox.dev                  # interactive
```

You can also stream over a one-off command — `ssh proj@host "tar czf - out" > out.tgz`.
(`rsync` over the front door isn't supported yet — it needs a raw exec channel;
use `scp`/`sftp` or the streaming form.)

## microVM & the image catalog {#microvm-image-catalog}

With the **microVM backend** (`--compute microvm`) every box is a real
Firecracker virtual machine — its own kernel, isolated from the host and from
other boxes — booting in well under a second from a **copy-on-write clone** of a
catalog image. The CoW disk is durable: it survives suspend/resume, a daemon
restart, and a host reboot.

The **image catalog** lives in `--fc-images-dir` as `<name>.ext4` files. You
build them once with the scripts in `build/microvm/`:

- **`build-rootfs.sh`** — composes a pinned Firecracker-CI base (Ubuntu 22.04),
  grows it, and bakes in `hopbox-agent`, `box-guest`, the in-VM init, and a set
  of dev tools (`git vim curl tmux python3` …).
- **`build-deboot.sh`** — debootstraps a full Debian (`debian-12`) or Ubuntu
  (`ubuntu-22.04`) system. Because it has a real dpkg database the box stays
  `apt`-extensible.

```sh
# one image:
sudo IMAGE=ubuntu-22.04 OUT_DIR=/opt/hopbox-microvm build/microvm/build-rootfs.sh
# a fully-tooled Debian image:
sudo DISTRO=debian build/microvm/build-deboot.sh
```

Users discover what you built with `ssh images@host`, and select one with
`ssh name:<image>@host`.

The **docker backend** (`--compute docker`) needs no kernel or catalog — boxes
are containers, any OCI image works, and `boxd` side-loads the agent +
`box-guest` binaries (`--agent-bin` / `--guest-bin`). It is the zero-setup
default; microVM is the stronger-isolation, suspend/resume option.

## Ephemeral vs persistent {#ephemeral-vs-persistent}

By default boxes are **ephemeral**: when you disconnect the box is reaped after
a short `--grace` window (default `2m`) so a reconnect or network blip doesn't
lose your shell. This is the anonymous, throwaway tier.

Run with **`--auto-suspend`** to make boxes **persistent** instead. An idle box
(`--idle-timeout`, default `5m`) is **suspended to disk** — memory snapshot plus
its durable CoW disk — and **resumes instantly** on your next connect, right
where you left off. On a clean daemon shutdown persistent boxes are drained
(suspended) so the next start resumes them rather than re-provisioning.

| | Ephemeral (default) | Persistent (`--auto-suspend`) |
| --- | --- | --- |
| On disconnect | reaped after `--grace` | kept; suspended when idle |
| On reconnect | fresh box (unless within grace) | resumes from snapshot |
| Survives daemon restart / host reboot | no | yes (durable disk + drain) |
| Best for | anonymous, throwaway | known users, real work |

::: tip hopbox vs boxd tiers
`boxd` applies one global tier (the `--auto-suspend` switch). The
[hopbox dev-env](/guide/what-is-hopbox) instead reads a **registered-keys file**
([`--accounts`](/reference/hopboxd#ssh-front-door)) so listed keys get the
persistent tier while everyone else stays ephemeral.
:::

## box-guest & MCP {#box-guest-mcp}

Every box ships **`box-guest`**, an in-box CLI for the box's own metadata API.
There is no credential — the metadata endpoint (`--meta-addr`) identifies the
calling box by its source IP.

```sh
box-guest info                 # this box's metadata (load, idle, resources)
box-guest time                 # the control plane's wall clock
box-guest keep-alive 30m       # pin the box alive (no suspend) for a while
box-guest auto-suspend on|off  # toggle auto-suspend for this box
box-guest idle 15m             # set this box's idle timeout (empty = default)
```

`box-guest mcp` runs an **MCP server** over stdio exposing those same operations
as tools (`box_info`, `box_keep_alive`, `box_auto_suspend`, `box_set_idle`) — so
an AI agent working inside a box can manage its own sandbox: keep itself alive
during a long task, then hand back to auto-suspend.

## Secure by default

Boxes are isolated and the egress is fenced without any extra scripting:

- **microVM** boxes are hardware-isolated VMs on a dedicated host bridge
  (`--fc-bridge`, default `hopbox-vmnet`; subnet `--fc-subnet`, default
  `10.0.0`). Run a second fleet beside another daemon by giving it a different
  bridge + subnet.
- A box reaches the agent hub and the internet, but **not** the host's other
  services, your LAN, or your tailnet.
- Anonymous boxes are capped (`--default-cpus`, `--default-mem-mb`) so one box
  can't exhaust the host.

## Install

One command on a Linux host with systemd:

```sh
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install-boxd.sh | sudo sh
```

It installs `boxd` + the in-box `hopbox-agent` + `box-guest`, writes a systemd
unit, and starts on the **docker** backend (zero extra setup). Config lives in
`/etc/hopbox/boxd.env`; edit and `systemctl restart boxd`.

For the **microVM** backend, install with `HOPBOX_COMPUTE=microvm` (needs
`/dev/kvm`), build the image catalog once, then restart:

```sh
curl -fsSL .../install-boxd.sh | sudo HOPBOX_COMPUTE=microvm sh
sudo IMAGE=ubuntu-22.04 OUT_DIR=/opt/hopbox-microvm build/microvm/build-rootfs.sh
sudo systemctl restart boxd
```

## Reference

- [`boxd` config](/reference/boxd) — every flag, exhaustively.
- [SSH & the front door](/guide/ssh) — the username grammar and the catalog.
- [Deploy a server](/guide/deploy) — the microVM host setup in detail.
