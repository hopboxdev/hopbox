# Quickstart

Get a workspace running and `ssh` into it in a few minutes. This walks through a
local Docker setup; for a real server see [Deploy a server](/guide/deploy).

::: tip Just want `ssh box@host`?
If you only want **compute boxes over SSH** — no CLI, no control plane to run —
try the standalone [boxd](/guide/boxd) layer: `ssh box@box.hopbox.dev` spawns a
microVM and drops you in. This quickstart is for the full hopbox dev-env.
:::

## Prerequisites

- **Docker** running locally (the default compute provider).
- The **`hopboxd`** (control plane) and **`hopbox`** (CLI) binaries.

### Install the CLI

The `hopbox` CLI is the client you run on your laptop.

**macOS** (Homebrew):

```sh
brew install hopboxdev/tap/hopbox
```

**macOS or Linux** (amd64 / arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install-cli.sh | sh
```

The script installs to `/usr/local/bin` (or `~/.local/bin` if that needs root).
Pin a version with `HOPBOX_VERSION=v0.2.0`, or grab an archive directly from
[Releases](https://github.com/hopboxdev/hopbox/releases)
(`hopbox_darwin_arm64.tar.gz`, …). You can also build from source with
`make build`.

`hopboxd` itself is Linux + Docker — run it locally on Linux (or in Docker), or
deploy it to a server with [`install.sh`](/guide/deploy).

## 1. Start the control plane

```sh
hopboxd
```

`hopboxd` listens on `localhost:7700` (API) and `:7777` (agent dial-in), uses the
Docker compute provider and a local home directory for storage, and creates an
SSH user CA at `./hopbox-ssh-ca` on first run. Leave it running.

## 2. Create a workspace

In another terminal:

```sh
hopbox create demo --image ubuntu:24.04
hopbox ls
```

The reconciler pulls the image, starts a container, and side-loads the agent,
which dials back to `hopboxd`. Within a few seconds the workspace reaches
`Running`.

## 3. Get a shell — the quick way

```sh
hopbox shell demo
```

This is an interactive PTY over the control plane (think `docker exec`), handy
for a quick look. For real SSH tooling, set up certificates next.

## 4. SSH in (and VS Code, scp, rsync)

```sh
hopbox login            # mints a short-lived SSH certificate from the CA
hopbox ssh-config demo  # writes an ~/.ssh entry for `demo`
ssh demo
```

Now `ssh demo`, **VS Code → Connect to Host → demo**, `scp`, and `rsync` all work
— no public port, no manual key wrangling. See [SSH & VS Code](/guide/ssh) for how
it works and how to wire VS Code.

## 5. Expose a web app

```sh
hopbox create web --image ubuntu:24.04 --expose app:3000
hopbox get web        # shows the resolved https:// endpoint
```

Run something on port 3000 inside the workspace and it's reachable at a
host-routed HTTPS URL.

## Clean up

```sh
hopbox rm demo
```

## Next

- [What is Hopbox](/guide/what-is-hopbox) — the architecture in five minutes.
- [boxd — compute over SSH](/guide/boxd) — the standalone `ssh box@host` layer.
- [Auth & multi-user](/guide/auth) — give each user their own boxes and keys.
- [Deploy a server](/guide/deploy) — run it for real with TLS.
