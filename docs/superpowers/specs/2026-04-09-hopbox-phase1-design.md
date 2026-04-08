# Hopbox Phase 1 Design — SSH Gateway + Docker Dev Containers

## Overview

Hopbox is a self-hosted SSH gateway that authenticates users by public key and drops them into isolated Docker-based dev containers. Phase 1 delivers the MVP: connect via SSH, auto-register on first use, land in a persistent container running zellij.

**Goal:** `ssh -p 2222 hop@server` → authenticated → inside a Docker container with zellij, neovim, node, python, and common CLI tools. Home directory persisted across reconnects.

## Architecture

Single Go binary (`hopboxd`) running on the host alongside Docker. Talks to the local Docker socket. No VPN, no sidecar containers.

### Connection Flow

```
User's machine                         Server (hopboxd)
─────────────                          ────────────────
ssh -p 2222 hop@server ──────────────► gliderlabs/ssh server
                                        │
                                        ├─ Public key auth
                                        │   ├─ Known fingerprint → lookup user
                                        │   ├─ Unknown + open_registration → TOFU registration TUI
                                        │   └─ Unknown + closed registration → reject
                                        │
                                        ├─ Parse username: "hop" or "hop+boxname"
                                        │   └─ boxname defaults to "default"
                                        │
                                        ├─ Container lifecycle
                                        │   ├─ Not found → create from hopbox-base image, start
                                        │   ├─ Stopped → start
                                        │   └─ Running → use as-is
                                        │
                                        ├─ docker exec -it: zellij attach --create default
                                        │   └─ stdin/stdout/stderr ↔ SSH channel
                                        │
                                        └─ Port forwarding (direct-tcpip)
                                            └─ localhost:N → container_ip:N
```

## SSH Server & Connection Handling

**Library:** `github.com/gliderlabs/ssh`

**Auth flow:**
1. Public key callback receives the key → compute SHA256 fingerprint
2. Look up fingerprint in user store
3. If found → auth succeeds, store user info in session context
4. If not found and `open_registration = true` → auth succeeds, flag session as "needs registration"
5. If not found and `open_registration = false` → auth rejected

**Username parsing:** SSH username parsed as `<user>` or `<user>+<boxname>`. Identity comes from key fingerprint (not the username field). The `boxname` part selects which devbox — defaults to `"default"` if omitted.

**Session handler:**
1. If flagged "needs registration" → run registration TUI, save to store, proceed
2. Look up or create container for user+boxname
3. Start container if stopped
4. `docker exec` with PTY into container, running `zellij attach --create default`
5. Pipe exec stream ↔ SSH channel until disconnect

**Host key management:**
- If `host_key_path` is configured and exists → use it
- If `host_key_path` is configured and missing → error, refuse to start
- If `host_key_path` is empty → auto-generate ed25519 key to `<data_dir>/host_key` on first run, log a warning

**Supported key types:** ed25519, ed25519-sk (FIDO2/YubiKey), rsa.

## User Store & Registration

**Storage:** File-based under `<data_dir>/users/`. Each user gets a directory named by key fingerprint (SHA256 hex digest with colons replaced by underscores, e.g., `SHA256_aa_bb_cc...`).

```
data/users/
└── SHA256_aa_bb_cc_dd.../
    ├── user.toml
    └── home/            # bind-mounted as /home/dev in containers
```

**`user.toml` format:**
```toml
username = "gandalf"
key_type = "ed25519-sk"
registered_at = 2026-04-09T12:00:00Z
```

**Registration (TOFU):** On first connection with an unknown key (when `open_registration = true`), hopboxd presents a `charmbracelet/huh` form over the SSH session asking for a username. Validates: alphanumeric + hyphens, unique across all users. Saves `user.toml` and creates the `home/` directory.

**Lookup:** On each connection, scan `data/users/*/user.toml` to build fingerprint→user map. Acceptable for Phase 1 scale.

## Container Management

### Base Image

On startup, hopboxd builds a base image from `templates/Dockerfile.base` + stack scripts. It hashes all template files → tags as `hopbox-base:<hash>`. Checks if tag exists on startup — rebuilds only if missing or hash changed.

**Phase 1 image contents:**
- Ubuntu 24.04
- sudo, curl, git, build-essential, openssh-client
- mise (runtime version manager)
- zellij, neovim
- Node LTS, Python 3.12 (via mise)
- fzf, ripgrep, fd, bat, lazygit, direnv
- `dev` user (UID 1000) with sudo, home at `/home/dev`

### Container Lifecycle

**Naming:** `hopbox-<username>-<boxname>` (e.g., `hopbox-gandalf-default`)

**On connect:**
- Container not found → create from `hopbox-base:<hash>`, start
- Container stopped → start
- Container running → use as-is

**Container config:**
- Image: `hopbox-base:<hash>`
- Bind mount: `data/users/<fingerprint>/home` → `/home/dev`
- User: `dev` (UID 1000)
- Working dir: `/home/dev`
- Entrypoint: `sleep infinity` (kept alive, we exec into it)

**On disconnect:** Container keeps running. Allows reconnecting to the same zellij session.

### Exec

`docker exec` with PTY, running: `zellij attach --create default`

This attaches to an existing zellij session or creates one. Reconnecting picks up where the user left off. stdin/stdout/stderr piped to the SSH channel.

## Port Forwarding

**Scope:** Local forwarding (`-L`) only. Remote forwarding (`-R`) and SOCKS (`-D`) are out of scope for Phase 1.

**Mechanism:** hopboxd implements gliderlabs/ssh's `LocalPortForwardingCallback` and handles `direct-tcpip` channel requests.

**Flow:**
1. User runs `ssh -p 2222 hop@server -L 3000:localhost:3000`
2. SSH client opens `direct-tcpip` channel requesting `localhost:3000`
3. hopboxd looks up the container IP for the current session via `ContainerInspect` → `NetworkSettings.IPAddress`
4. Dials `<container_ip>:3000` from the host
5. Pipes TCP connection ↔ SSH channel

**Isolation:** Container IP lookup is per-session. Two users both forwarding `-L 3000:localhost:3000` route to different container IPs. No host port publishing, no collisions.

## Configuration

**`config.toml`:**
```toml
port = 2222
data_dir = "./data"
host_key_path = ""           # empty = auto-generate to <data_dir>/host_key
open_registration = true
```

Loaded from `./config.toml` by default, overridable with `--config` flag. Missing file uses defaults.

## Project Structure

```
hopbox/
├── cmd/
│   └── hopboxd/
│       └── main.go                # parse config, start server
├── internal/
│   ├── gateway/
│   │   ├── server.go              # SSH server setup, session handler
│   │   ├── username.go            # parse "hop+boxname" format
│   │   └── tunnel.go              # direct-tcpip handler (port forwarding)
│   ├── users/
│   │   ├── store.go               # fingerprint→user lookup, registration
│   │   └── register.go            # TOFU registration TUI (huh form)
│   ├── containers/
│   │   ├── manager.go             # container lifecycle (create, start, exec)
│   │   └── image.go               # base image build + hash check
│   └── config/
│       └── config.go              # config.toml parsing, defaults
├── templates/
│   ├── Dockerfile.base            # ubuntu 24.04 + all Phase 1 tools
│   └── stacks/
│       ├── tools.sh               # fzf, ripgrep, fd, bat, lazygit, direnv
│       └── runtimes.sh            # mise + node LTS + python 3.12
├── data/                          # gitignored, runtime state
├── go.mod
└── go.sum
```

## Key Dependencies

- `github.com/gliderlabs/ssh` — SSH server
- `github.com/docker/docker/client` — Docker SDK for Go
- `github.com/charmbracelet/huh` — TUI form for registration
- `github.com/pelletier/go-toml/v2` — TOML parsing
- `golang.org/x/crypto/ssh` — underlying SSH primitives

## What Phase 1 Does NOT Include

- Interactive tool selection wizard (Phase 2)
- Multiple devboxes per user with picker TUI (Phase 3)
- Admin CLI commands (Phase 4)
- Idle timeout auto-stop (Phase 4)
- Resource limits (Phase 4)
- Remote forwarding / SOCKS proxy
- Default zellij/neovim configs (can add as a fast follow)
