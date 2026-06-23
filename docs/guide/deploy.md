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

## Enable multi-user / SSO

Point `hopboxd` at a users file or your IdP — see [Auth & multi-user](/guide/auth)
and the [`hopboxd` config reference](/reference/hopboxd).

::: info Expanding
This page is a starting point. A complete, opinionated production guide
(hardening, backups, upgrades) is in progress.
:::
