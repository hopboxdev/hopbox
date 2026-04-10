# Hopbox Phase 5D Design — GitHub Releases & One-Command Install

## Overview

Phase 5D adds a GitHub Actions release workflow that builds tagged releases as tarballs, and an install script that downloads, installs, and upgrades hopbox with one command on a Linux server. Optional `--with-monitoring` flag sets up Prometheus + Grafana with auto-provisioned dashboards.

**Goal:** `curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash` installs hopbox from the latest release. Same command upgrades an existing install. `--with-monitoring` adds the observability stack.

## Versioning

Semver tags (`v0.1.0`, `v0.2.0`). Each tag push triggers a GitHub Actions workflow that builds binaries and publishes a release.

## Release Artifacts

Each release has two tarballs:
```
hopbox-v0.1.0-linux-amd64.tar.gz
hopbox-v0.1.0-linux-arm64.tar.gz
```

Tarball contents:
```
hopbox-v0.1.0/
├── hopboxd                            # server binary (built for target arch)
├── templates/
│   ├── Dockerfile.base                # base image Dockerfile
│   └── hopbox                         # in-container CLI (built for target arch)
├── config.example.toml
├── deploy/
│   ├── hopboxd.service                # systemd unit
│   └── monitoring/
│       ├── compose.yml
│       ├── prometheus.yml
│       └── grafana/
│           ├── provisioning/
│           │   ├── datasources/prometheus.yml
│           │   └── dashboards/hopbox.yml
│           └── dashboards/
│               ├── server-overview.json
│               └── box-details.json
└── VERSION                            # contains "v0.1.0"
```

## GitHub Actions Release Workflow

`.github/workflows/release.yml`:
- **Trigger:** push of tags matching `v*`
- **Jobs:** build for `linux/amd64` and `linux/arm64` in a matrix
- **Steps per matrix entry:**
  1. Checkout repo
  2. Set up Go
  3. Build `hopboxd` with `GOOS=linux GOARCH=$arch CGO_ENABLED=0`
  4. Build in-container `hopbox` CLI with same env
  5. Assemble release directory structure (`hopbox-$version/` with binaries, templates, deploy files)
  6. Create tarball `hopbox-$version-linux-$arch.tar.gz`
  7. Upload as build artifact
- **Final job (depends on matrix):**
  - Downloads both tarballs
  - Creates GitHub release using `softprops/action-gh-release` with both tarballs attached
  - Release body is auto-generated from commits since previous tag

## Install Script

Located at `scripts/install.sh` in the repo. Fetched via:

```bash
# Latest release
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash

# Specific version
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash -s -- v0.1.0

# With monitoring stack
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash -s -- --with-monitoring

# Both flags
curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash -s -- v0.1.0 --with-monitoring
```

### Script behavior

1. **Preflight checks:**
   - Must run as root (or via sudo)
   - Must be Linux
   - Detect arch: `uname -m` → `amd64` or `arm64`
   - Check Docker is installed (`docker --version`), fail with clear message if not
   - If `--with-monitoring`, check `docker compose` is available

2. **Resolve version:**
   - If version arg provided, use it
   - Otherwise, query GitHub API: `https://api.github.com/repos/hopboxdev/hopbox/releases/latest`
   - Extract `tag_name`

3. **Download and extract:**
   - URL: `https://github.com/hopboxdev/hopbox/releases/download/$version/hopbox-$version-linux-$arch.tar.gz`
   - Download to `/tmp/hopbox-$version.tar.gz`
   - Extract to `/opt/hopbox/` (creates `/opt/hopbox/$version/`)
   - Clean up tarball

4. **Create user and directories:**
   - Create `hopbox` system user if missing: `useradd -r -s /usr/sbin/nologin -d /opt/hopbox hopbox`
   - Add to docker group: `usermod -aG docker hopbox`
   - Create `/etc/hopbox/` if missing
   - Create `/var/lib/hopbox/` if missing
   - `chown -R hopbox:hopbox /var/lib/hopbox /opt/hopbox/$version`

5. **Install config (only on fresh install):**
   - If `/etc/hopbox/config.toml` does not exist, copy from `/opt/hopbox/$version/config.example.toml`
   - Otherwise leave existing config alone (upgrade preserves config)

6. **Install systemd unit:**
   - Copy `/opt/hopbox/$version/deploy/hopboxd.service` to `/etc/systemd/system/hopboxd.service`
   - `systemctl daemon-reload`

7. **Update symlinks:**
   - `ln -sfn /opt/hopbox/$version /opt/hopbox/current`
   - `ln -sfn /opt/hopbox/current/hopboxd /usr/local/bin/hopboxd`

8. **Start/restart service:**
   - `systemctl enable hopboxd`
   - If hopboxd was running, `systemctl restart hopboxd`, else `systemctl start hopboxd`
   - Check status, report success

9. **If `--with-monitoring`:**
   - Copy `/opt/hopbox/$version/deploy/monitoring/` to `/opt/hopbox/monitoring/` (not versioned — Grafana data persists across hopbox upgrades)
   - Keep existing `prometheus.yml` and Grafana provisioning if files already exist
   - `cd /opt/hopbox/monitoring && docker compose up -d`
   - Report Grafana URL and default credentials

10. **Print summary:**
    ```
    Hopbox v0.1.0 installed successfully.
    
    Config: /etc/hopbox/config.toml
    Data:   /var/lib/hopbox/
    Binary: /usr/local/bin/hopboxd
    Service: systemctl status hopboxd
    
    Next steps:
      1. Edit /etc/hopbox/config.toml (set hostname, admin password, etc.)
      2. Restart: systemctl restart hopboxd
      3. Connect: ssh -p 2222 hop@your-server
    ```

## Grafana Auto-Provisioning

New provisioning config files bundled with releases so Grafana auto-loads datasource and dashboards on first start.

**`deploy/monitoring/grafana/provisioning/datasources/prometheus.yml`:**
```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: true
```

**`deploy/monitoring/grafana/provisioning/dashboards/hopbox.yml`:**
```yaml
apiVersion: 1
providers:
  - name: 'Hopbox'
    orgId: 1
    folder: 'Hopbox'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
```

Dashboard JSON files are copied from `deploy/grafana/` into `deploy/monitoring/grafana/dashboards/` during the release build (or duplicated in the repo — same files).

**Updated `deploy/monitoring/compose.yml`** — mount provisioning dirs:
```yaml
grafana:
  volumes:
    - ./grafana/provisioning:/etc/grafana/provisioning
    - ./grafana/dashboards:/var/lib/grafana/dashboards
    - grafana-data:/var/lib/grafana
```

## Upgrade Flow

Running the install script on an existing install:
1. Detects new version vs `/opt/hopbox/current` target
2. Downloads new tarball, extracts to new versioned dir
3. Updates `current` symlink (atomic)
4. Restarts service
5. Leaves old version dir in place for manual rollback (`ln -sfn /opt/hopbox/v0.1.0 /opt/hopbox/current && systemctl restart hopboxd`)

Config and data are preserved (separate paths).

## Rollback

Manual rollback:
```bash
sudo ln -sfn /opt/hopbox/v0.1.0 /opt/hopbox/current
sudo systemctl restart hopboxd
```

Old versions accumulate in `/opt/hopbox/`. The install script does NOT auto-prune old versions — admin cleans them manually.

## Filesystem Layout

```
/opt/hopbox/
├── v0.1.0/                # versioned install
│   ├── hopboxd
│   ├── templates/
│   ├── config.example.toml
│   └── deploy/
├── v0.2.0/                # previous version (kept for rollback)
├── current → v0.1.0       # symlink to active version
└── monitoring/            # monitoring stack (if installed, not versioned)
    ├── compose.yml
    ├── prometheus.yml
    └── grafana/
        ├── provisioning/
        └── dashboards/

/usr/local/bin/hopboxd → /opt/hopbox/current/hopboxd
/etc/systemd/system/hopboxd.service
/etc/hopbox/config.toml
/var/lib/hopbox/
├── host_key
└── users/
    └── SHA256_.../
```

## File Changes

**New files:**
```
.github/workflows/release.yml                         # release CI
scripts/install.sh                                    # install/upgrade script
scripts/build-release.sh                              # build tarball (used by CI and local)
deploy/monitoring/grafana/
├── provisioning/
│   ├── datasources/prometheus.yml
│   └── dashboards/hopbox.yml
└── dashboards/                                       # copied from deploy/grafana/
    ├── server-overview.json
    └── box-details.json
```

**Modified files:**
```
deploy/monitoring/compose.yml                         # mount provisioning dirs
```

## What Phase 5D Does NOT Include

- Automatic Docker installation (admin must install first)
- Firewall rule management (admin handles)
- Certificate management / HTTPS for admin UI (use a reverse proxy)
- Auto-rollback on failure (manual only)
- Release notes automation beyond auto-generated commit log
- Checksum/signature verification of downloaded binaries (future)
