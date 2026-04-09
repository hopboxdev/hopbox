# Hopbox Phase 5A Design — Port Expose Helper & Admin Web UI

## Overview

Phase 5A adds a `hopbox expose` command that prints SSH tunnel instructions, and an admin web UI for managing users and boxes.

**Goal:** `hopbox expose 3000` prints the SSH tunnel command for the user. The admin web UI at `http://server:8080` provides a dashboard, user/box management, and a registration toggle.

## `hopbox expose`

Simple CLI command that prints SSH tunnel instructions. No socket communication needed.

**Usage:**
```
$ hopbox expose 3000
To access port 3000 from your machine, run:

  ssh -p 2222 -L 3000:localhost:3000 -N hop@dev.example.com

Then open http://localhost:3000
```

**Hostname resolution:** The server's `hostname` config field is passed to containers via the control socket's status response. The CLI reads it from status data. If no hostname is configured, falls back to `<server>` as a placeholder.

**Config addition:**
```toml
hostname = "dev.example.com"  # empty = show <server> placeholder
```

**CLI addition (kong):**
```go
type ExposeCmd struct {
    Port int `arg:"" help:"Port to expose."`
}
```

## Admin Web UI

### Config

```toml
[admin]
enabled = true
port = 8080
username = "admin"
password = "changeme"
```

Default: disabled. Must be explicitly enabled with credentials set.

### Authentication

HTTP Basic Auth middleware. Credentials from `config.toml`. Every request requires auth.

### Pages

**Dashboard (`GET /`)**
- Total users, total boxes, running containers count
- Server uptime, base image tag
- Server hostname, SSH port

**Users (`GET /users`)**
- Table: username, key type, registered date, number of boxes
- Action: Remove user button (htmx DELETE with confirmation)

**User Boxes (`GET /users/{username}/boxes`)**
- Table: box name, container status (running/stopped/none), profile summary (shell/multiplexer)
- Actions: Stop container button, Remove box button (htmx with confirmation)

**Settings (`GET /settings`)**
- Toggle open_registration on/off (htmx PUT, runtime only, doesn't persist to config file)

### htmx Actions

- `DELETE /api/users/{username}` — remove user, all boxes, all containers
- `DELETE /api/users/{username}/boxes/{boxname}` — remove box and container
- `POST /api/users/{username}/boxes/{boxname}/stop` — stop container
- `PUT /api/settings/registration` — toggle open_registration

All actions return HTML fragments that htmx swaps into the page.

### Tech Stack

- `net/http` with Go standard mux
- `html/template` with `embed` for templates
- htmx via CDN (single JS file, no build step)
- Tailwind CSS via CDN (no build step)
- Basic auth middleware

### File Structure

```
internal/admin/
├── server.go              # HTTP server, routes, basic auth middleware
├── handlers.go            # page handlers + API action handlers
├── templates/
│   ├── layout.html        # base layout with nav, tailwind CDN, htmx CDN
│   ├── dashboard.html
│   ├── users.html
│   ├── boxes.html
│   └── settings.html
```

## Config Changes

```toml
# Server hostname (used in SSH tunnel instructions and admin UI)
hostname = ""

[admin]
enabled = false
port = 8080
username = "admin"
password = ""
```

## Modified Files

```
internal/config/config.go      # add Hostname, AdminConfig
cmd/hopboxd/main.go            # start admin HTTP server
cmd/hopbox/main.go             # add expose command
internal/control/handler.go    # add hostname + port to status response
```

## What This Does NOT Include

- HTTPS (use a reverse proxy like Caddy for TLS)
- Persistent settings changes (toggle is runtime only)
- Log viewing in the UI
- Image management in the UI
