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

## Reference

- [`hopbox` CLI](/reference/cli) — `login`, `ssh`, `ssh-config`, `proxy`.
- [Auth & multi-user](/guide/auth) — certificates, the CA, and multi-user.
