# Deploy a server

Run Hopbox on a server so your workspaces are reachable beyond your laptop, with
real TLS for the HTTPS gateway.

## One-command install

```sh
curl -fsSL https://hopbox.dev/install.sh | sudo sh
```

The installer drops the `hopboxd` / `hopbox-gw` / `hopbox-agent` binaries, sets up
a systemd service, and gets the control plane running. See the
[`deploy/`](https://github.com/hopboxdev/hopbox/tree/main/deploy) directory for
the script, a sample `Caddyfile`, and the full guide.

## TLS for the gateway

The HTTPS gateway needs a wildcard cert for your `--gateway-zone` (so every
workspace gets `app-*.gw.yourdomain`). The `deploy/` guide covers terminating TLS
with Caddy (on-demand or wildcard) in front of `hopbox-gw`.

## Firecracker microVM backend

By default workspaces are Docker containers. To run them as **Firecracker
microVMs** instead — hardware isolation, snapshot suspend/resume — start
`hopboxd` with `--compute microvm`. The host needs **`/dev/kvm`** (bare metal or
a nested-virt VPS) and two things in place:

1. **A kernel + image catalog.** Build them once with the scripts in
   `build/microvm/` — the same catalog format both `hopboxd` and `boxd` use:

   ```sh
   # pinned Ubuntu base + agent/box-guest baked in:
   sudo IMAGE=ubuntu-22.04 OUT_DIR=/opt/hopbox-microvm build/microvm/build-rootfs.sh
   # a fully apt-extensible Debian image:
   sudo DISTRO=debian build/microvm/build-deboot.sh
   ```

   This writes `/opt/hopbox-microvm/vmlinux` and
   `/opt/hopbox-microvm/images/<name>.ext4`.

2. **The flags** pointing at them:

   ```sh
   hopboxd --compute microvm \
     --fc-kernel /opt/hopbox-microvm/vmlinux \
     --fc-images-dir /opt/hopbox-microvm/images \
     --home-size-mb 4096
   ```

The microVM fleet runs on its own host bridge (`--fc-bridge`, default
`hopbox-vmnet`) and `/24` (`--fc-subnet`, default `10.0.0`); set both to run a
fleet beside another daemon. See the [microVM flags](/reference/hopboxd#microvm-firecracker).

## Just want `ssh box@host`?

If you don't need the dev-env platform — workspaces, the gateway, the CLI — and
only want **compute boxes over SSH**, deploy the standalone
[**boxd**](/guide/boxd) daemon instead. One command:

```sh
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install-boxd.sh | sudo sh
```

It installs `boxd` + the in-box agent + `box-guest`, writes a systemd unit, and
starts on the docker backend (or microVM with `HOPBOX_COMPUTE=microvm`). See
[boxd → install](/guide/boxd#install).

## Enable multi-user / SSO

Point `hopboxd` at a users file or your IdP — see [Auth & multi-user](/guide/auth)
and the [`hopboxd` config reference](/reference/hopboxd).

::: info Expanding
This page is a starting point. A complete, opinionated production guide
(hardening, backups, upgrades) is in progress.
:::
