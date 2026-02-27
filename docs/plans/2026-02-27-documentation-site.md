# Documentation Site Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the hopbox.dev documentation site using Docusaurus v3 with a landing page and 15 docs pages, all in a `website/` directory within the monorepo.

**Architecture:** Docusaurus v3 with TypeScript, bun as package manager, custom React landing page, markdown docs organized into 4 sections (Getting Started, Guides, Reference, Architecture). Static output served by nginx/caddy on the user's VPS.

**Tech Stack:** Docusaurus v3, React, TypeScript, bun, CSS Modules.

---

### Task 1: Scaffold Docusaurus project and configure

**Files:**
- Create: `website/` (via `bunx create-docusaurus`)
- Modify: `website/docusaurus.config.ts`
- Modify: `website/sidebars.ts`
- Create: `website/src/css/custom.css`
- Modify: `.gitignore`

**Step 1: Scaffold the project**

```bash
cd /path/to/hopbox
bunx create-docusaurus@latest website classic --typescript
```

This creates the full Docusaurus scaffold under `website/`.

**Step 2: Remove default content**

Delete the generated placeholder content:

```bash
rm -rf website/docs/*
rm -rf website/blog
rm -rf website/src/pages/index.tsx
rm -rf website/src/components/HomepageFeatures/
```

**Step 3: Write `website/docusaurus.config.ts`**

Replace the generated config entirely:

```ts
import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Hopbox',
  tagline: 'Instant dev environments on your own VPS',
  favicon: 'img/favicon.ico',
  url: 'https://hopbox.dev',
  baseUrl: '/',
  organizationName: 'hopboxdev',
  projectName: 'hopbox',
  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/hopboxdev/hopbox/tree/main/website/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      title: 'Hopbox',
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          href: 'https://github.com/hopboxdev/hopbox',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Getting Started', to: '/docs/getting-started/installation'},
            {label: 'Guides', to: '/docs/guides/setup'},
            {label: 'Reference', to: '/docs/reference/cli'},
          ],
        },
        {
          title: 'More',
          items: [
            {label: 'GitHub', href: 'https://github.com/hopboxdev/hopbox'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Hopbox. AGPL-3.0 License.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
```

**Step 4: Write `website/sidebars.ts`**

Replace the generated sidebars:

```ts
import type {SidebarsConfig} from '@docusaurus/types';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    {
      type: 'category',
      label: 'Getting Started',
      items: [
        'getting-started/installation',
        'getting-started/quickstart',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      items: [
        'guides/setup',
        'guides/workspace-lifecycle',
        'guides/services',
        'guides/bridges',
        'guides/snapshots',
        'guides/migration',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/cli',
        'reference/manifest',
        'reference/environment',
        'reference/agent-api',
      ],
    },
    {
      type: 'category',
      label: 'Architecture',
      items: [
        'architecture/overview',
        'architecture/wireguard-tunnel',
        'architecture/helper-daemon',
      ],
    },
  ],
};

export default sidebars;
```

**Step 5: Write `website/src/css/custom.css`**

```css
:root {
  --ifm-color-primary: #2563eb;
  --ifm-color-primary-dark: #1d4ed8;
  --ifm-color-primary-darker: #1e40af;
  --ifm-color-primary-darkest: #1e3a8a;
  --ifm-color-primary-light: #3b82f6;
  --ifm-color-primary-lighter: #60a5fa;
  --ifm-color-primary-lightest: #93c5fd;
  --ifm-code-font-size: 95%;
  --docusaurus-highlighted-code-line-bg: rgba(0, 0, 0, 0.1);
}

[data-theme='dark'] {
  --ifm-color-primary: #3b82f6;
  --ifm-color-primary-dark: #2563eb;
  --ifm-color-primary-darker: #1d4ed8;
  --ifm-color-primary-darkest: #1e40af;
  --ifm-color-primary-light: #60a5fa;
  --ifm-color-primary-lighter: #93c5fd;
  --ifm-color-primary-lightest: #bfdbfe;
  --docusaurus-highlighted-code-line-bg: rgba(0, 0, 0, 0.3);
}
```

**Step 6: Update `.gitignore`**

Append to the root `.gitignore`:

```
# Documentation site
website/node_modules/
website/build/
website/.docusaurus/
```

**Step 7: Create placeholder doc so the build passes**

Create `website/docs/getting-started/installation.md`:

```markdown
---
sidebar_position: 1
---

# Installation

Coming soon.
```

**Step 8: Verify it builds**

```bash
cd website && bun install && bun run build
```

Expected: build succeeds, static output in `website/build/`.

**Step 9: Commit**

```bash
git add website/ .gitignore
git commit -m "feat: scaffold Docusaurus documentation site"
```

---

### Task 2: Create landing page

**Files:**
- Create: `website/src/pages/index.tsx`
- Create: `website/src/pages/index.module.css`

**Step 1: Write `website/src/pages/index.module.css`**

```css
.hero {
  padding: 4rem 2rem;
  text-align: center;
}

.heroTitle {
  font-size: 3rem;
  font-weight: 800;
}

.heroSubtitle {
  font-size: 1.25rem;
  color: var(--ifm-color-emphasis-700);
  max-width: 600px;
  margin: 1rem auto;
}

.buttons {
  display: flex;
  gap: 1rem;
  justify-content: center;
  margin-top: 2rem;
}

.features {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 2rem;
  padding: 4rem 2rem;
  max-width: 1100px;
  margin: 0 auto;
}

.feature {
  padding: 1.5rem;
}

.featureTitle {
  font-size: 1.25rem;
  font-weight: 700;
  margin-bottom: 0.5rem;
}

.featureDescription {
  color: var(--ifm-color-emphasis-700);
}

.terminal {
  max-width: 700px;
  margin: 0 auto;
  padding: 2rem;
}

.terminalWindow {
  background: #1e1e1e;
  border-radius: 8px;
  overflow: hidden;
}

.terminalBar {
  background: #333;
  padding: 8px 12px;
  display: flex;
  gap: 6px;
}

.terminalDot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #555;
}

.terminalBody {
  padding: 1rem 1.5rem;
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 0.875rem;
  line-height: 1.6;
  color: #d4d4d4;
  overflow-x: auto;
}

.terminalBody code {
  background: none;
  padding: 0;
  color: inherit;
}

.terminalComment {
  color: #6a9955;
}

.terminalCommand {
  color: #dcdcaa;
}

.terminalOutput {
  color: #9cdcfe;
}
```

**Step 2: Write `website/src/pages/index.tsx`**

```tsx
import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

function Hero() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <h1 className={styles.heroTitle}>{siteConfig.title}</h1>
      <p className={styles.heroSubtitle}>
        Instant dev environments on your own VPS — no cloud accounts,
        no coordination server, no monthly seat fee.
      </p>
      <div className={styles.buttons}>
        <Link className="button button--primary button--lg" to="/docs/getting-started/installation">
          Get Started
        </Link>
        <Link className="button button--secondary button--lg" href="https://github.com/hopboxdev/hopbox">
          GitHub
        </Link>
      </div>
    </header>
  );
}

const features = [
  {
    title: 'WireGuard Tunnel',
    description:
      'A private L3 network between your laptop and VPS. Every port is directly reachable — no per-port SSH forwarding.',
  },
  {
    title: 'Workspace Manifest',
    description:
      'A single hopbox.yaml declares packages, services, bridges, env vars, scripts, backups, and sessions.',
  },
  {
    title: 'Workspace Mobility',
    description:
      'Snapshot your workspace, migrate to a new host with one command. Your data follows you across providers.',
  },
  {
    title: 'Hybrid Services',
    description:
      'Run Docker containers and native processes side by side, with health checks, dependencies, and log aggregation.',
  },
];

function Features() {
  return (
    <section className={styles.features}>
      {features.map((feature) => (
        <div key={feature.title} className={styles.feature}>
          <h3 className={styles.featureTitle}>{feature.title}</h3>
          <p className={styles.featureDescription}>{feature.description}</p>
        </div>
      ))}
    </section>
  );
}

function TerminalDemo() {
  return (
    <section className={styles.terminal}>
      <div className={styles.terminalWindow}>
        <div className={styles.terminalBar}>
          <span className={styles.terminalDot} />
          <span className={styles.terminalDot} />
          <span className={styles.terminalDot} />
        </div>
        <div className={styles.terminalBody}>
          <div><span className={styles.terminalComment}># Bootstrap your VPS</span></div>
          <div><span className={styles.terminalCommand}>$ hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/key</span></div>
          <div><span className={styles.terminalOutput}>  Installing hop-agent... done</span></div>
          <div><span className={styles.terminalOutput}>  Exchanging WireGuard keys... done</span></div>
          <div><span className={styles.terminalOutput}>  Host "mybox" saved as default.</span></div>
          <br />
          <div><span className={styles.terminalComment}># Bring up the tunnel</span></div>
          <div><span className={styles.terminalCommand}>$ hop up</span></div>
          <div><span className={styles.terminalOutput}>  WireGuard tunnel... up</span></div>
          <div><span className={styles.terminalOutput}>  Agent probe... healthy</span></div>
          <div><span className={styles.terminalOutput}>  Syncing workspace... done</span></div>
          <div><span className={styles.terminalOutput}>  Agent is up.</span></div>
        </div>
      </div>
    </section>
  );
}

export default function Home(): React.JSX.Element {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout title={siteConfig.title} description={siteConfig.tagline}>
      <Hero />
      <main>
        <Features />
        <TerminalDemo />
      </main>
    </Layout>
  );
}
```

**Step 3: Verify build**

```bash
cd website && bun run build
```

Expected: builds successfully.

**Step 4: Commit**

```bash
git add website/src/pages/
git commit -m "feat: add documentation site landing page"
```

---

### Task 3: Write Getting Started docs (2 pages)

**Files:**
- Create: `website/docs/getting-started/installation.md`
- Create: `website/docs/getting-started/quickstart.md`

**Step 1: Write `website/docs/getting-started/installation.md`**

Content covers 4 installation methods: install script, Homebrew, `go install`, build from source. Also covers the helper daemon installation step (required on first use). Reference: README lines 35-43 for current install instructions, `internal/helper/install_darwin.go` and `install_linux.go` for helper details.

Structure:
```markdown
---
sidebar_position: 1
---

# Installation

## Requirements

| | Minimum |
|---|---|
| **Developer machine** | macOS or Linux |
| **VPS** | Any Linux with systemd and a public IP |
| **SSH access** | Key-based auth to the VPS |

## Install the CLI

### Option 1: Install script

(curl | sh one-liner from GitHub releases)

### Option 2: Homebrew

(brew install hopboxdev/tap/hop)

### Option 3: Go install

(go install github.com/hopboxdev/hopbox/cmd/hop@latest)

### Option 4: Build from source

(git clone, make build)

## Helper daemon

(Explain that hop-helper is installed automatically on first `hop setup` — it handles TUN device creation and /etc/hosts management. On macOS it runs as a LaunchDaemon, on Linux as a systemd service. Requires sudo once.)

## Verify installation

(hop version)
```

**Step 2: Write `website/docs/getting-started/quickstart.md`**

Adapted from README quickstart section. Walks through the 3-step flow: install, setup, up. Also shows a minimal `hopbox.yaml` and `hop status`.

Structure:
```markdown
---
sidebar_position: 2
---

# Quickstart

## 1. Install hop

(Link to installation page)

## 2. Bootstrap your VPS

(hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/key)
(Explain what it does: SSH in, install agent, exchange WG keys, save config)

## 3. Bring up the tunnel

(hop up)
(Explain: tunnel is up when you see "Agent is up.")

## 4. Create a workspace (optional)

(Show minimal hopbox.yaml with a package and service)
(hop up again to sync the manifest)

## 5. Check status

(hop status — explain the dashboard output)

## Next steps

(Links to guides: setup details, services, bridges, snapshots)
```

**Step 3: Verify build**

```bash
cd website && bun run build
```

**Step 4: Commit**

```bash
git add website/docs/getting-started/
git commit -m "docs: add getting started pages (installation, quickstart)"
```

---

### Task 4: Write Guides (6 pages)

**Files:**
- Create: `website/docs/guides/setup.md`
- Create: `website/docs/guides/workspace-lifecycle.md`
- Create: `website/docs/guides/services.md`
- Create: `website/docs/guides/bridges.md`
- Create: `website/docs/guides/snapshots.md`
- Create: `website/docs/guides/migration.md`

Write each page with the content described below. Source material for each page is listed. Write for a developer audience — clear, concise, with code examples. Each page should be 100-300 lines of markdown.

**`guides/setup.md`** (sidebar_position: 1)
- Detailed `hop setup` walkthrough
- SSH trust-on-first-use (TOFU) — user confirms host key fingerprint
- Agent binary download and installation as systemd service
- WireGuard key generation and exchange
- Host config saved to `~/.config/hopbox/hosts/<name>.yaml`
- Default host auto-set on first setup
- Flags: `-a` (address), `-u` (user), `-k` (keyfile), `-p` (port)
- Source: `README.md` setup section, `CLAUDE.md` setup docs, `cmd/hop/setup.go`

**`guides/workspace-lifecycle.md`** (sidebar_position: 2)
- `hop up` — brings up WireGuard tunnel, syncs manifest, starts bridges and services
- Runs as background daemon; `hop down` tears it down
- Reconnection resilience — 5s heartbeat, auto-recovery
- `hop status` — dashboard showing tunnel state, CONNECTED, LAST HEALTHY
- Host resolution order: `--host` flag > `host:` in hopbox.yaml > default_host in config
- Source: `README.md` architecture section, `CLAUDE.md` reconnection section

**`guides/services.md`** (sidebar_position: 3)
- Docker services: `type: docker`, image, ports, env, data mounts, health checks
- Native services: `type: native`, command, workdir
- Dependency ordering with `depends_on`
- Port binding: default to WireGuard IP (10.10.0.2), 3-part format for public
- `hop services ls/restart/stop`, `hop logs`
- Source: `README.md` hopbox.yaml section, `internal/manifest/manifest.go` Service struct

**`guides/bridges.md`** (sidebar_position: 4)
- What bridges are: local resources exposed to the remote workspace
- clipboard bridge: bidirectional clipboard sync
- cdp bridge: Chrome DevTools Protocol proxy on port 9222
- xdg-open bridge: remote `xdg-open` opens URLs in local browser
- notifications bridge: remote notifications appear on local desktop
- Configuration in hopbox.yaml `bridges:` section
- `hop bridge ls/restart`
- Source: `CLAUDE.md` bridge system section, `internal/bridge/`

**`guides/snapshots.md`** (sidebar_position: 5)
- Restic-based workspace snapshots
- `hop snap create` — captures service data directories
- `hop snap ls` — list available snapshots
- `hop snap restore <id>` — restore from snapshot
- Backup config in hopbox.yaml: backend, target (s3, local, etc.)
- Source: `README.md` commands section, `internal/snapshot/`

**`guides/migration.md`** (sidebar_position: 6)
- `hop to <newhost>` workflow
- 3-phase migration: snapshot current, bootstrap new host, restore on new
- Prerequisites: new host must be set up with `hop setup` first
- Error recovery: idempotent retry on failure
- Source: `CLAUDE.md` hop to docs, `docs/plans/2026-02-20-hop-to-design.md`

**Step: Verify build**

```bash
cd website && bun run build
```

**Step: Commit**

```bash
git add website/docs/guides/
git commit -m "docs: add guide pages (setup, lifecycle, services, bridges, snapshots, migration)"
```

---

### Task 5: Write Reference docs (4 pages)

**Files:**
- Create: `website/docs/reference/cli.md`
- Create: `website/docs/reference/manifest.md`
- Create: `website/docs/reference/environment.md`
- Create: `website/docs/reference/agent-api.md`

**`reference/cli.md`** (sidebar_position: 1)
- Complete CLI reference with all commands, subcommands, flags, and examples
- Commands to document (each with synopsis, description, flags, example):
  - `hop setup <name> -a <ip> [-u user] [-k keyfile] [-p port]`
  - `hop up [workspace]`
  - `hop down`
  - `hop status`
  - `hop code [path]`
  - `hop run <script>`
  - `hop services [ls|restart|stop]`
  - `hop logs [service]`
  - `hop snap [create|restore|ls]`
  - `hop to <newhost>`
  - `hop bridge [ls|restart]`
  - `hop host [add|rm|ls|default]`
  - `hop upgrade [--version V] [--local]`
  - `hop rotate [host]`
  - `hop init`
  - `hop version`
- Global flags: `-H`/`--host`, `-v`/`--verbose`
- Host resolution order
- Source: `README.md` commands section, `CLAUDE.md` CLI commands section, `cmd/hop/main.go`

**`reference/manifest.md`** (sidebar_position: 2)
- Full `hopbox.yaml` schema reference
- Document every field with type, default, and description:
  - `name` (string, required)
  - `host` (string, optional)
  - `packages` (list of Package)
  - `services` (map of Service)
  - `bridges` (list of Bridge)
  - `env` (map string→string)
  - `scripts` (map string→string)
  - `backup` (BackupConfig)
  - `editor` (EditorConfig)
  - `session` (SessionConfig)
- Nested types: Package (name, backend, version, url, sha256), Service (type, image, command, workdir, ports, env, health, data, depends_on), HealthCheck (http, interval, timeout), DataMount (host, container), Bridge (type), BackupConfig (backend, target), SessionConfig (manager, name), EditorConfig (type, path, extensions)
- Complete example hopbox.yaml
- Source: `internal/manifest/manifest.go`, `README.md` hopbox.yaml section

**`reference/environment.md`** (sidebar_position: 3)
- `.env` and `.env.local` file loading
- Precedence: `.env` < `.env.local` < manifest `env:` < service-level `env:`
- Files must be placed next to `hopbox.yaml`
- `.env.local` is gitignored by convention
- Services are recreated on `hop up` so env changes take effect immediately
- Source: `README.md` env section, `CLAUDE.md` dotenv section, `internal/dotenv/`

**`reference/agent-api.md`** (sidebar_position: 4)
- Agent control API reference
- Base URL: `http://<name>.hop:4200` (only accessible over WireGuard)
- `GET /health` — returns health status JSON
- `POST /rpc` — JSON-RPC dispatcher
- RPC methods table: services.list, services.restart, services.stop, ports.list, run.script, logs.stream, packages.install, snap.create, snap.restore, snap.list, workspace.sync
- Note: `logs.stream` returns `text/plain` (streaming), all others return JSON
- Request/response format examples
- Source: `CLAUDE.md` agent API section, `internal/agent/api.go`

**Step: Verify build**

```bash
cd website && bun run build
```

**Step: Commit**

```bash
git add website/docs/reference/
git commit -m "docs: add reference pages (CLI, manifest, environment, agent API)"
```

---

### Task 6: Write Architecture docs (3 pages)

**Files:**
- Create: `website/docs/architecture/overview.md`
- Create: `website/docs/architecture/wireguard-tunnel.md`
- Create: `website/docs/architecture/helper-daemon.md`

**`architecture/overview.md`** (sidebar_position: 1)
- Three Go binaries: `cmd/hop/` (client CLI), `cmd/hop-agent/` (server daemon), `cmd/hop-helper/` (privileged helper)
- Communication model: WireGuard L3 tunnel (UDP) is primary transport, SSH only for bootstrap
- Agent API on `<name>.hop:4200` over WireGuard — never exposed publicly
- Point-to-point topology: no coordination server, no DERP relay
- Hostname convention: `<name>.hop` added to `/etc/hosts` by helper
- Architecture diagram (ASCII art from README/product-overview)
- Source: `README.md` architecture section, `docs/product-overview.md` architecture section, `CLAUDE.md` architecture section

**`architecture/wireguard-tunnel.md`** (sidebar_position: 2)
- Client WireGuard mode: kernel TUN (utun on macOS, hopbox%d on Linux) via helper daemon
- Server WireGuard mode: kernel TUN (preferred, CAP_NET_ADMIN), netstack fallback
- Netstack for `hop to` only (temporary migration tunnels, avoids routing conflicts)
- Key exchange during `hop setup` over SSH; all subsequent communication over WireGuard
- Key rotation via `hop rotate` without full re-setup
- Reconnection resilience: 5s heartbeat via ConnMonitor, auto-recovery, state file
- WireGuard IPs: client=10.10.0.1, server=10.10.0.2
- Source: `CLAUDE.md` architecture and reconnection sections

**`architecture/helper-daemon.md`** (sidebar_position: 3)
- Why a helper daemon: TUN device creation and IP/route config require root privileges
- Unix socket protocol at `/var/run/hopbox/helper.sock`
- SCM_RIGHTS fd passing: helper creates TUN fd, passes it to unprivileged client
- macOS: LaunchDaemon at `/Library/LaunchDaemons/dev.hopbox.helper.plist`, utun via AF_SYSTEM socket, `ifconfig`/`route` for IP config
- Linux: systemd service at `/etc/systemd/system/hopbox-helper.service`, wireguard-go `tun.CreateTUN`, `ip` command for IP config
- Actions: create_tun, configure_tun, cleanup_tun, add_host, remove_host, version
- `/etc/hosts` management: adds `<name>.hop` entries so the agent is reachable by hostname
- Source: `CLAUDE.md` architecture section, `internal/helper/protocol.go`, `internal/helper/tun_darwin.go`, `internal/helper/tun_linux.go`

**Step: Verify build**

```bash
cd website && bun run build
```

**Step: Commit**

```bash
git add website/docs/architecture/
git commit -m "docs: add architecture pages (overview, tunnel, helper daemon)"
```

---

### Task 7: Add Makefile targets, verify full build, update ROADMAP

**Files:**
- Modify: `Makefile`
- Modify: `ROADMAP.md`

**Step 1: Add docs targets to Makefile**

Append after the existing `tidy` target:

```makefile
# ── Documentation ────────────────────────────────────────────────────────────

.PHONY: docs
docs:
	cd website && bun run build

.PHONY: docs-dev
docs-dev:
	cd website && bun start

.PHONY: docs-deploy
docs-deploy:
	cd website && bun run build && rsync -avz build/ hopbox.dev:/var/www/hopbox.dev/
```

**Step 2: Run final full build**

```bash
cd website && bun run build
```

Expected: builds with no warnings or errors, output in `website/build/`.

**Step 3: Run Go tests to make sure nothing is broken**

```bash
go test ./...
```

Expected: all packages pass (website/ is not Go code and won't interfere).

**Step 4: Update ROADMAP.md**

Change:
```
- [ ] Documentation site (hopbox.dev) — quickstart, manifest reference, migration guides
```

To:
```
- [x] Documentation site (hopbox.dev) — Docusaurus, landing page, 15 docs pages
```

**Step 5: Commit**

```bash
git add Makefile ROADMAP.md
git commit -m "docs: add Makefile targets and mark docs site complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `website/` | Create — full Docusaurus v3 project scaffold |
| `website/docusaurus.config.ts` | Modify — site config, navbar, footer, theme |
| `website/sidebars.ts` | Modify — 4-section docs sidebar |
| `website/src/css/custom.css` | Create — theme color overrides |
| `website/src/pages/index.tsx` | Create — landing page (hero, features, terminal) |
| `website/src/pages/index.module.css` | Create — landing page styles |
| `website/docs/getting-started/installation.md` | Create — install methods |
| `website/docs/getting-started/quickstart.md` | Create — 3-step quickstart |
| `website/docs/guides/setup.md` | Create — hop setup walkthrough |
| `website/docs/guides/workspace-lifecycle.md` | Create — hop up/down/status |
| `website/docs/guides/services.md` | Create — Docker + native services |
| `website/docs/guides/bridges.md` | Create — clipboard, CDP, xdg-open, notifications |
| `website/docs/guides/snapshots.md` | Create — restic snapshots |
| `website/docs/guides/migration.md` | Create — hop to workflow |
| `website/docs/reference/cli.md` | Create — full CLI reference |
| `website/docs/reference/manifest.md` | Create — hopbox.yaml schema |
| `website/docs/reference/environment.md` | Create — env var loading |
| `website/docs/reference/agent-api.md` | Create — HTTP/RPC API |
| `website/docs/architecture/overview.md` | Create — 3-binary architecture |
| `website/docs/architecture/wireguard-tunnel.md` | Create — tunnel internals |
| `website/docs/architecture/helper-daemon.md` | Create — helper protocol |
| `.gitignore` | Modify — add website build artifacts |
| `Makefile` | Modify — add docs/docs-dev/docs-deploy targets |
| `ROADMAP.md` | Modify — check off documentation site |
