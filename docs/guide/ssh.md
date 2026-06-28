# SSH & VS Code

A Hopbox workspace is a first-class SSH host: `ssh`, VS Code Remote-SSH, `scp`,
`rsync`, JetBrains Gateway — anything that speaks SSH works, with **no public
port** and no manual key distribution.

## How it works

The workspace agent dials *out* to the control plane and serves an SSH server
over that existing reverse tunnel — nothing routes *into* the box. The `hopbox`
CLI bridges your local `ssh` to it:

```
ssh mybox → hopbox proxy (ProxyCommand) → hopboxd → agent's sshd
```

Authentication is a short-lived **SSH certificate** (see
[Auth & multi-user](/guide/auth)). The agent's host key persists on the
workspace's home volume, so there are no `known_hosts` prompts across restarts.

## One-time setup

```sh
hopbox login            # fetch a certificate for your identity
hopbox ssh-config mybox # write an ~/.ssh entry
```

`hopbox ssh-config` writes `~/.ssh/hopbox/mybox.config` and adds an
`Include hopbox/*.config` line to your `~/.ssh/config`. The entry sets the right
`User`, your Hopbox identity file, and `ProxyCommand hopbox proxy`.

## Connect

```sh
ssh mybox
```

Or without writing any config:

```sh
hopbox ssh mybox            # interactive
hopbox ssh mybox -- uname -a
```

## VS Code

Install the **Remote - SSH** extension, then:

1. Run `hopbox ssh-config mybox` once (so VS Code can see the host).
2. Command Palette → **Remote-SSH: Connect to Host…** → `mybox`.

VS Code installs its server over the SSH connection (the agent implements the
SFTP subsystem) and opens the workspace.

## scp / rsync / git

Because it's a normal SSH host, file transfer just works:

```sh
scp ./file mybox:/home/dev/
rsync -a ./src/ mybox:/home/dev/src/
```

## The SSH front door

The flow above reaches a workspace you already created. hopboxd can also run a
**front door**: a plain SSH listener where the **username is a box spec** and the
**client key is the identity** — no signup, no pre-created workspace. Enable it
with [`--ssh-addr`](/reference/hopboxd#ssh-front-door). (This is the same front
door that the standalone [boxd](/guide/boxd) daemon is built around.)

```sh
ssh proj@host                 # spawn/attach box "proj" (default image)
ssh proj:ubuntu-22.04@host    # pick a catalog image
ssh proj:debian-12:cpu+5m@host # image + flavor, stay alive 5m after you disconnect
ssh proj~docker@host          # pin the docker backend
ssh proj+@host                # force a fresh box
ssh images@host               # list the image catalog (spawns no box)
```

### Username grammar

```
name[~backend][:image[:flavor[+duration]]]
```

| Segment | Meaning |
| --- | --- |
| `name` | Box name, created on first connect. Names are **per-owner** — your `proj` is distinct from another key's `proj`. Append `+` to force a fresh box. |
| `~backend` | Compute backend (`docker`, `microvm`, `kubernetes`). Omit = auto: the sole backend, or the configured default. |
| `:image` | Box image. With docker, any OCI ref. With microVM, a catalog name (`ssh images@host` lists them). Omit = `--ssh-default-image` (`alpine`). |
| `:flavor` | Hardware flavor. A recognized named flavor (e.g. `:medium`) sets the box's CPU/memory caps, overriding the front-door defaults. |
| `+duration` | Stay-alive grace after disconnect (`5m`, `1h`). Omit = the daemon default grace. |

`images` (or `image`) is a **meta-command**: it prints the available image
catalog and spawns no box. Select one with `ssh name:<image>@host`.

### Lifetime — ephemeral vs persistent

Front-door boxes are **ephemeral by default**: the box is attached for the life
of your SSH session and reaped a short grace window after you disconnect. A
reconnect within that window (your `+duration`, or the daemon default) cancels
the reap, so a network blip never loses your shell.

A **registered key** gets the **persistent tier** instead: the box auto-suspends
to disk when idle and **resumes instantly** on your next connect — durable
across daemon restarts and host reboots. You register keys with the
[`--accounts`](/reference/hopboxd#ssh-front-door) file (`<ssh-key> <account>`
per line); keys not listed stay anonymous and ephemeral. See
[Auth → the account tier](/guide/auth#the-front-door-account-tier).

### Identity & visibility

The front door has **no signup**: your SSH **key is your identity** and the box
is owned by that key's fingerprint. This is a separate namespace from your named
CLI login, so:

- **Front-door boxes do not appear in `hopbox ls`.** `ls` is scoped to your CLI
  principal (your `hopbox login` identity), while a front-door box is owned by
  your raw key fingerprint — a different owner. The box is real and running; it
  is just not listed under your named identity. This is by design: front-door
  boxes are anonymous and ephemeral, not part of your persistent workspace set.
- **Reconnecting with the same key** reuses the same box (within its lifetime);
  a different key connecting to the same name is refused while that box is alive.

For boxes you manage with the CLI (`hopbox ls`, `rm`, `ssh`, VS Code), create
them with [`hopbox create`](/reference/cli) instead of the front door.

## Reference

- [`hopbox` CLI](/reference/cli) — `login`, `ssh`, `ssh-config`, `proxy`.
- [Auth & multi-user](/guide/auth) — certificates, the CA, and multi-user.
- [`hopboxd` config](/reference/hopboxd) — `--ssh-addr` and the front door flags.
