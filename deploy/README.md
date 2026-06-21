# Deploying Hopbox (single server)

A one-command installer for a Linux server with Docker. It installs the control
plane (`hopboxd`) and gateway (`hopbox-gw`) as systemd services, wires the
docker-bridge firewall rule, and optionally configures Caddy for wildcard HTTPS.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install.sh | sudo sh
```

First run with options (control-plane + gateway only):

```sh
curl -fsSL .../install.sh | sudo HOPBOX_ZONE=gw.example.com sh
```

With automatic wildcard HTTPS via Caddy (Caddy must be installed):

```sh
curl -fsSL .../install.sh | sudo HOPBOX_ZONE=gw.example.com HOPBOX_CADDY=1 sh
```

The installer is **idempotent** — re-run it to upgrade binaries or re-apply
config. Settings live in `/etc/hopbox/hopbox.env`; edit and run
`systemctl restart hopboxd hopbox-gw` (or re-run the installer).

## What it sets up

| Path | What |
|------|------|
| `/usr/local/bin/{hopboxd,hopbox,hopbox-gw}` | control plane, CLI, gateway |
| `/var/lib/hopbox/` | sqlite DB + the injected `hopbox-agent` binary |
| `/etc/hopbox/hopbox.env` | configuration (edit + restart to apply) |
| `/etc/systemd/system/hopbox*.service` | services (read the env file) |

Ports: API `127.0.0.1:7700` (private — unauthenticated for now), agent `:7777`,
tunnel `127.0.0.1:7701`, gateway `127.0.0.1:8088`, on-demand-TLS ask `:8089`.

## DNS + TLS

- Point a **wildcard** record `*.<zone>` (e.g. `*.gw.example.com`) at the
  server's public IP. Workspaces are then reachable at
  `<name>-<id>.<zone>` — Host-routed, so one cert covers all of them.
- For TLS, run with `HOPBOX_CADDY=1` (Caddy on-demand issues a cert per
  subdomain, bounded to your zone), or front `hopbox-gw` with your own
  terminator. See `Caddyfile.example`.

## Using it

```sh
hopbox --addr 127.0.0.1:7700 create demo --image ubuntu:24.04 --expose app:8000
hopbox --addr 127.0.0.1:7700 exec demo -- uname -a
hopbox --addr 127.0.0.1:7700 shell demo
# -> https://app-<id>.<zone>
```

The API is unauthenticated until the Identity provider is wired — keep `:7700`
private and reach it over SSH (`ssh -L 7700:127.0.0.1:7700 <server>`).

## Uninstall

```sh
sudo systemctl disable --now hopboxd hopbox-gw
sudo rm -f /etc/systemd/system/hopbox{d,-gw}.service /usr/local/bin/{hopboxd,hopbox,hopbox-gw}
sudo rm -rf /var/lib/hopbox /etc/hopbox
sudo systemctl daemon-reload
```
