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

## Ephemeral front door

The flow above reaches a workspace you already created. hopboxd can also run a
**front door**: a plain SSH listener where the **username is a workspace spec**
and the **client key is the identity** — no signup, no pre-created workspace.
Enable it with [`--ssh-addr`](/reference/hopboxd#ssh-front-door).

```sh
ssh proj@host                 # spawn/attach workspace "proj" (default image)
ssh proj:python@host          # python image
ssh proj:go:cpu+5m@host       # go image, stay alive 5m after you disconnect
ssh proj~docker:python@host   # pin the docker backend
ssh proj+@host                # force a fresh box
```

### Username grammar

```
workspace[~backend][:image[:flavor[+duration]]]
```

| Segment | Meaning |
| --- | --- |
| `workspace` | Workspace name (created on first connect). Append `+` to force a fresh box. |
| `~backend` | Compute backend (`docker`, `kubernetes`, …). Omit = auto: the sole backend, or the configured default. |
| `:image` | Box image. Omit = `--ssh-default-image` (`alpine`). |
| `:flavor` | Hardware flavor (reserved; not yet applied). |
| `+duration` | Stay-alive grace after disconnect (`5m`, `1h`). Omit = reap immediately. |

### Lifetime

Front-door boxes are **ephemeral**: the workspace is attached for the life of
your SSH session and reaped when you disconnect (after the `+duration` grace, if
any). A reconnect within the grace window cancels the reap. This is the
temporary-box model — for a persistent workspace, create one via the
[`hopbox` CLI](/reference/cli) instead.

## Reference

- [`hopbox` CLI](/reference/cli) — `login`, `ssh`, `ssh-config`, `proxy`.
- [Auth & multi-user](/guide/auth) — certificates, the CA, and multi-user.
- [`hopboxd` config](/reference/hopboxd) — `--ssh-addr` and the front door flags.
