# Production Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make hopboxd deployable as a systemd service with clean shutdown and a README for self-hosters.

**Architecture:** Add `Manager.Shutdown()` for cleanup, wire it into the signal handler, create a systemd unit file, and write a comprehensive README.

**Tech Stack:** Go, systemd, Markdown

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/containers/manager.go` | Add `Shutdown()` method |
| `cmd/hopboxd/main.go` | Call `Shutdown()` in signal handler |
| `deploy/hopboxd.service` | Systemd unit file |
| `README.md` | User-facing documentation |

---

### Task 1: Graceful Shutdown

**Files:**
- Modify: `internal/containers/manager.go`
- Modify: `cmd/hopboxd/main.go`

- [ ] **Step 1: Add Shutdown method to Manager**

Add to `internal/containers/manager.go`:

```go
// Shutdown cleans up all socket servers and cancels all idle timers.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, srv := range m.sockets {
		log.Printf("[shutdown] closing socket server for container %s", id[:12])
		srv.Close()
	}
	m.sockets = make(map[string]*control.SocketServer)

	for id, s := range m.states {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
			log.Printf("[shutdown] cancelled idle timer for container %s", id[:12])
		}
	}
	m.states = make(map[string]*containerState)
}
```

- [ ] **Step 2: Update main.go signal handler**

Replace the signal handler in `cmd/hopboxd/main.go` (lines 78-85):

```go
	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		mgr.Shutdown()
		srv.Close()
		log.Println("shutdown complete")
	}()
```

- [ ] **Step 3: Verify it compiles and tests pass**

```bash
go build ./... && go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/containers/manager.go cmd/hopboxd/main.go
git commit -m "feat: add graceful shutdown with socket and timer cleanup"
```

---

### Task 2: Systemd Unit File

**Files:**
- Create: `deploy/hopboxd.service`

- [ ] **Step 1: Create the unit file**

Create `deploy/hopboxd.service`:

```ini
[Unit]
Description=Hopbox SSH Dev Environment Gateway
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=hopbox
Group=hopbox
WorkingDirectory=/opt/hopbox
ExecStart=/usr/local/bin/hopboxd --config /etc/hopbox/config.toml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/hopbox /tmp
ReadOnlyPaths=/opt/hopbox /etc/hopbox

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Commit**

```bash
git add deploy/hopboxd.service
git commit -m "feat: add systemd unit file for hopboxd"
```

---

### Task 3: README

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

Create `README.md`:

```markdown
# Hopbox

Self-hosted SSH gateway that drops users into isolated Docker-based dev containers. Connect via SSH, pick your tools, and land in a persistent environment with zellij, your editor, and your runtimes.

## Features

- **SSH-native** — `ssh hop@server` is all you need. No VPN, no browser.
- **Per-user isolation** — each user gets their own Docker container with a persistent home directory.
- **Tool selection wizard** — choose your multiplexer, editor, shell, runtimes, and CLI tools on first connect.
- **Multiple devboxes** — `ssh hop+project@server` for separate environments. `ssh hop+?@server` to pick.
- **TOFU registration** — new SSH keys are auto-registered with an interactive username prompt.
- **Idle timeout** — containers auto-stop after configurable hours of inactivity.
- **Resource limits** — CPU, memory, and PID limits per container.

## Requirements

- Linux server (Ubuntu 22.04+ recommended)
- Docker Engine 24+
- Go 1.24+ (for building from source)

## Quick Start

```bash
# Build
git clone https://github.com/hopboxdev/hopbox.git
cd hopbox
go build -o hopboxd ./cmd/hopboxd/
./scripts/build-cli.sh  # cross-compile in-container CLI

# Run
./hopboxd
# First run builds the base Docker image (~2 min)
# Listens on :2222 by default

# Connect
ssh -p 2222 hop@localhost
# Register a username, pick your tools, land in your container
```

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

## Usage

### Connecting

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

Inside your container, the `hopbox` command is available:

```bash
hopbox status          # show box info
hopbox status --json   # JSON output
hopbox destroy         # destroy this box (with confirmation)
```

## Deployment

### Systemd (recommended)

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
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "feat: add README with installation, configuration, and deployment guide"
```

---

## Task Dependency Graph

```
Task 1 (Shutdown) ──► Task 2 (Systemd) ──► Task 3 (README)
```

Tasks 2 and 3 are independent of each other but both logically follow Task 1.
