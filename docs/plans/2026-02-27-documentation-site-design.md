# Documentation Site Design (hopbox.dev)

## Goal

Create a documentation website for Hopbox at `hopbox.dev` with a marketing landing page and comprehensive documentation covering installation, usage guides, reference material, and architecture.

## Decisions

- **Framework:** Docusaurus v3
- **Package manager:** bun
- **Location:** `website/` directory in the monorepo
- **URL structure:** `hopbox.dev/` (landing page) + `hopbox.dev/docs/` (documentation)
- **Hosting:** Self-hosted on VPS, static files served by nginx/caddy
- **Deploy:** `bun run build` produces static output in `website/build/`, rsync to VPS

## Project Structure

```
website/
  docusaurus.config.ts    -- site config (title, navbar, footer, theme)
  sidebars.ts             -- docs sidebar structure
  package.json            -- Docusaurus + bun deps
  src/
    pages/
      index.tsx           -- custom landing page
    css/
      custom.css          -- theme overrides
  docs/
    getting-started/
      installation.md
      quickstart.md
    guides/
      setup.md
      workspace-lifecycle.md
      migration.md
      snapshots.md
      bridges.md
      services.md
    reference/
      manifest.md
      cli.md
      agent-api.md
      environment.md
    architecture/
      overview.md
      wireguard-tunnel.md
      helper-daemon.md
  static/
    img/                  -- logo, diagrams
```

The root `.gitignore` adds `website/node_modules/` and `website/build/`.

## Landing Page

Custom React page at `src/pages/index.tsx` with four sections:

1. **Hero** -- tagline ("Instant dev environments on your own VPS"), one-liner description, two CTA buttons: "Get Started" (links to docs quickstart) and "GitHub" (links to repo).
2. **Feature cards** (3-4 cards) -- Wireguard tunnel, workspace manifest, workspace mobility, hybrid services. Short descriptions adapted from `docs/product-overview.md`.
3. **Terminal demo** -- styled code block showing the `hop setup` -> `hop up` -> `hop status` flow, giving visitors an immediate feel for the developer experience.
4. **Footer** -- links to docs sections, GitHub repo, license.

Uses Docusaurus built-in CSS modules for styling. No Tailwind or external CSS framework.

## Documentation Content

15 pages across 4 sections:

### Getting Started (2 pages)

| Page | Content |
|------|---------|
| `installation.md` | Install script, Homebrew, `go install`, build from source |
| `quickstart.md` | Bootstrap a VPS and bring up the tunnel (adapted from README) |

### Guides (6 pages)

| Page | Content |
|------|---------|
| `setup.md` | Detailed `hop setup` walkthrough -- SSH TOFU, agent install, key exchange |
| `workspace-lifecycle.md` | `hop up` / `hop down` / `hop status` flow, daemon mode, reconnection |
| `migration.md` | `hop to` workflow -- snapshot, bootstrap new host, restore |
| `snapshots.md` | `hop snap create/restore/ls`, restic backend, backup targets |
| `bridges.md` | Clipboard, Chrome CDP, xdg-open, notifications bridges |
| `services.md` | Docker + native services in hopbox.yaml, health checks, dependencies |

### Reference (4 pages)

| Page | Content |
|------|---------|
| `manifest.md` | Full `hopbox.yaml` schema reference -- all fields, types, defaults |
| `cli.md` | All CLI commands with flags and examples |
| `agent-api.md` | HTTP/JSON-RPC endpoints, RPC methods, request/response formats |
| `environment.md` | `.env` / `.env.local` loading, precedence rules, service env merge |

### Architecture (3 pages)

| Page | Content |
|------|---------|
| `overview.md` | Three-binary architecture, communication model, topology |
| `wireguard-tunnel.md` | Kernel TUN, netstack fallback, reconnection resilience, key rotation |
| `helper-daemon.md` | macOS LaunchDaemon, Linux systemd, SCM_RIGHTS fd passing, socket protocol |

## Content Sourcing

- **Adapted from README:** quickstart, CLI commands, env vars, architecture overview
- **Adapted from product-overview.md:** landing page feature descriptions
- **Adapted from CLAUDE.md:** agent API endpoints, architecture details
- **New content:** installation, all guides, manifest reference, tunnel details, helper daemon details

"Adapted" means rewriting for a docs audience, not copy-paste.

## Build and Deploy

### Makefile targets

```makefile
docs:           ## Build documentation site
    cd website && bun run build

docs-dev:       ## Start docs dev server with hot reload
    cd website && bun start

docs-deploy:    ## Deploy docs to VPS
    cd website && bun run build && rsync -avz build/ user@hopbox.dev:/var/www/hopbox.dev/
```

### Local development

```bash
cd website
bun install
bun start        # opens http://localhost:3000
```

### gitignore additions

```
website/node_modules/
website/build/
website/.docusaurus/
```
