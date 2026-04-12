# Hopbox

Self-hosted SSH gateway that drops users into isolated Docker-based dev containers. Connect via SSH, pick your tools, and land in a persistent environment with zellij, your editor, and your runtimes.

## Features

- **SSH-native** — `ssh hop@server` is all you need. No VPN, no browser.
- **Per-user isolation** — each user gets their own Docker container with a persistent home directory.
- **Tool selection wizard** — choose your multiplexer, editor, shell, runtimes, and CLI tools on first connect.
- **Multiple devboxes** — `ssh hop+project@server` for separate environments. `ssh hop+?@server` to pick.
- **Shared boxes across keys** — `hopbox link` lets a second device join an existing box.
- **TOFU registration** — new SSH keys are auto-registered with an interactive username prompt.
- **Idle timeout** — containers auto-stop after configurable hours of inactivity.
- **Resource limits** — CPU, memory, and PID limits per container.
- **Admin web UI** — dashboard for users, boxes, and settings with HTTP basic auth.
- **Observability** — structured logs, `/healthz`, Prometheus metrics, ready-made Grafana dashboards.

## Requirements

- Linux server (Ubuntu 22.04+ recommended)
- Docker Engine 24+
- Go 1.24+ (for building from source)

## Quick Start

### Fresh VPS (one command, sets up everything)

On a brand-new Ubuntu/Debian VPS with nothing installed:

```bash
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/provision-vps.sh | sudo bash
```

This installs Docker, configures UFW (allows 22/tcp and 2222/tcp), then runs the hopbox installer. It intentionally does **not** touch `/etc/ssh/sshd_config` or create admin users to avoid lockout — harden OpenSSH yourself afterwards. Supports the same flags as the install script (`v0.1.0`, `--with-monitoring`).

### One-command install (Linux with Docker already set up)

```bash
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash
```

This downloads the latest release, installs hopboxd to `/usr/local/bin`, drops a config at `/etc/hopbox/config.toml`, and starts the systemd service on port 2222. Re-run the same command to upgrade.

Optional flags:

```bash
# Pin to a specific version
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash -s -- v0.1.0

# Also start Prometheus + Grafana with pre-provisioned dashboards
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash -s -- --with-monitoring
```

Then connect:

```bash
ssh -p 2222 hop@your-server
```

### Build from source

```bash
git clone https://github.com/hopboxdev/hopbox.git
cd hopbox
make run   # builds hopboxd + in-container CLI and runs it
```

Listens on `:2222` by default. First run builds the base Docker image (~2 min).

## Configuration

Copy the example config and modify as needed:

```bash
cp config.example.toml config.toml
```

Key options:

| Option | Default | Description |
|--------|---------|-------------|
| `port` | `2222` | SSH server port |
| `data_dir` | `./data` | User data, profiles, home directories |
| `host_key_path` | (auto-generate) | Path to SSH host key |
| `open_registration` | `true` | Allow new users to self-register |
| `idle_timeout_hours` | `24` | Hours before idle containers stop (0 = disabled) |
| `resources.cpu_cores` | `2` | CPU cores per container |
| `resources.memory_gb` | `4` | RAM per container (GB) |
| `resources.pids_limit` | `512` | Max processes per container |

See [config.example.toml](config.example.toml) for the full annotated config.

## Client CLI

The `hop` CLI wraps SSH/SCP so you don't have to remember flags and ports.

### Install

```bash
# Homebrew (macOS / Linux)
brew tap hopboxdev/tap
brew install hop

# Or with Go
go install github.com/hopboxdev/hopbox/cmd/hop@latest

# Or download from GitHub releases
curl -L https://github.com/hopboxdev/hopbox/releases/latest/download/hop-darwin-arm64 -o hop
chmod +x hop && sudo mv hop /usr/local/bin/
```

### Setup

```bash
hop init
# Server hostname: [hopbox.dev]:
# SSH port [2222]:
# Default box: [default]:
```

### Commands

```bash
hop                    # SSH into your default box
hop -b work            # SSH into a specific box
hop expose 3000        # forward box:3000 to localhost:3000
hop transfer file.txt  # upload a file to ~/
hop config             # show resolved configuration
```

### Raw SSH (without the CLI)

```bash
# Default box
ssh -p 2222 hop@server

# Named box
ssh -p 2222 hop+myproject@server

# Box picker (when you have multiple boxes)
ssh -p 2222 hop+?@server
```

### First Connection

New SSH keys trigger registration:
1. Choose a username
2. Select your tools (multiplexer, editor, shell, runtimes, CLI tools)
3. Wait for your environment to build
4. Land in your dev container running zellij (or tmux)

### Reconnecting

Subsequent connections skip the wizard and go straight to your container. Your zellij/tmux session persists — disconnect and reconnect without losing state.

### Port Forwarding

Forward ports from your container to your local machine:

```bash
ssh -p 2222 -L 3000:localhost:3000 hop@server
```

Each user's tunnels are isolated — no port collisions.

### In-Container CLI

Inside your container, the `hop` command is available:

```bash
hop status          # show box info (user, box, container, resources)
hop expose 3000     # print the SSH tunnel command for a local port
hop link            # generate a one-time code to add another SSH key to this box
hop destroy         # destroy this box (with confirmation)
```

### Sharing a box across devices

From inside a box on your first device:

```bash
hop link
# → code: ABCD-1234 (valid 5 min)
```

From your second device:

```bash
ssh -p 2222 hop@server
# the wizard offers "Link to existing box" — paste the code
```

The second key is linked to the same user and boxes via a filesystem symlink, so both devices share the same container and home directory.

## Admin Web UI

Set `[admin].enabled = true` in the config (disabled by default) and hopboxd serves a small htmx/Tailwind admin UI on port 8080:

- **Dashboard** — user, box, and container counts
- **Users** — registered users and their keys
- **Boxes** — running boxes with live resource usage
- **Settings** — current config (read-only)

Protected by HTTP basic auth (`admin.username` / `admin.password` in config).

> **Security note:** the admin server binds `:8080` on all interfaces. `/healthz` and `/metrics` on the same port are **unauthenticated**. Block 8080 at the firewall (the provisioning script already does) and front it with a reverse proxy that handles TLS — see [`deploy/caddy/Caddyfile.example`](deploy/caddy/Caddyfile.example) for a ready-to-use setup.

Unauthenticated endpoints on the same listener:
- `GET /healthz` — liveness probe; pings Docker
- `GET /metrics` — Prometheus metrics

## Monitoring

A ready-to-run Prometheus + Grafana stack lives under [`deploy/monitoring/`](deploy/monitoring/). It's bundled into release tarballs, and the install script sets it up for you with `--with-monitoring`.

Both services bind to `127.0.0.1` only — reach them over an SSH tunnel or front them with a reverse proxy.

For local development:

```bash
make monitoring-up      # starts Prometheus on :9090 and Grafana on :3000
# Grafana default login: admin / admin
make monitoring-down
```

Grafana comes pre-provisioned with two dashboards:
- **Hopbox — Server Overview** — users, boxes, build durations, running containers with drill-down
- **Hopbox — Box Details** — per-box CPU, memory, network, and disk IO

## Putting it behind a domain

Release tarballs ship a ready-to-use Caddy config at `/opt/hopbox/current/deploy/caddy/Caddyfile.example` and a static landing page at `/opt/hopbox/current/web/landing/`. To wire up `hopbox.dev`, `admin.hopbox.dev`, and `grafana.hopbox.dev`:

```bash
# Install Caddy (see https://caddyserver.com/docs/install)
sudo mkdir -p /var/www/hopbox
sudo cp -R /opt/hopbox/current/web/landing/. /var/www/hopbox/
sudo cp /opt/hopbox/current/deploy/caddy/Caddyfile.example /etc/caddy/Caddyfile
# Edit /etc/caddy/Caddyfile: replace hopbox.dev with your domain and paste a bcrypt hash
# from: caddy hash-password --plaintext 'your-grafana-password'
sudo systemctl reload caddy
```

Open `80/tcp` and `443/tcp` in UFW (`sudo ufw allow 80/tcp && sudo ufw allow 443/tcp`). Caddy handles Let's Encrypt automatically.

## Deployment

Most users should use the [install script](#quick-start). The steps below document what it does in case you want to install manually.

### Systemd (manual)

1. **Create a hopbox user:**

```bash
sudo useradd -r -s /usr/sbin/nologin -d /opt/hopbox hopbox
sudo usermod -aG docker hopbox
```

2. **Install the binary and templates:**

```bash
sudo mkdir -p /opt/hopbox /etc/hopbox /var/lib/hopbox
sudo cp hopboxd /usr/local/bin/
sudo cp -r templates /opt/hopbox/
sudo cp config.example.toml /etc/hopbox/config.toml
sudo chown -R hopbox:hopbox /opt/hopbox /var/lib/hopbox
```

3. **Edit the config:**

```bash
sudo vim /etc/hopbox/config.toml
# Set data_dir = "/var/lib/hopbox"
# Adjust resources and timeout as needed
```

4. **Install and start the service:**

```bash
sudo cp deploy/hopboxd.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now hopboxd
```

5. **Check status:**

```bash
sudo systemctl status hopboxd
sudo journalctl -u hopboxd -f
```

### Firewall

Open the SSH port (default 2222):

```bash
sudo ufw allow 2222/tcp
```

Consider also listening on port 443 for corporate firewall bypass.

### Host Key

By default, hopboxd auto-generates an ED25519 host key on first run. For production, pre-generate one:

```bash
ssh-keygen -t ed25519 -f /etc/hopbox/host_key -N ""
```

Then set `host_key_path = "/etc/hopbox/host_key"` in your config.

## Architecture

Hopbox is a single Go binary (`hopboxd`) that runs an SSH server using [charmbracelet/ssh](https://github.com/charmbracelet/ssh). Users authenticate by SSH public key. Each user gets Docker containers created from a shared base image (Ubuntu 24.04 + mise) with per-user tool layers built from their profile.

```
SSH Client → hopboxd (auth, wizard, container lifecycle) → Docker containers
                                                              ├── hopbox-gandalf-default
                                                              ├── hopbox-gandalf-project1
                                                              └── hopbox-aragorn-default
```

Data is stored on the filesystem under `data_dir`:
- `users/<fingerprint>/user.toml` — username and key info
- `users/<fingerprint>/profile.toml` — default tool profile
- `users/<fingerprint>/boxes/<boxname>/profile.toml` — per-box profile override
- `users/<fingerprint>/boxes/<boxname>/home/` — bind-mounted as `/home/dev`

## License

MIT
