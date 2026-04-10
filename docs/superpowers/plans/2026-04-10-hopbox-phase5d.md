# Phase 5D Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build and publish hopbox releases via GitHub Actions, install with one command.

**Architecture:** GitHub Actions builds versioned tarballs for linux/amd64 and linux/arm64, attaches to GitHub releases. A shell install script on the server downloads the release, extracts to /opt/hopbox/<version>/, updates symlinks, and manages systemd + optional monitoring stack.

**Tech Stack:** Bash, GitHub Actions, Go cross-compilation, Docker Compose, systemd

---

## Reference

Full design spec: `docs/superpowers/specs/2026-04-10-hopbox-phase5d-design.md`

Current repo layout (relevant bits):
- Go module: `github.com/hopboxdev/hopbox` (Go 1.24)
- Server binary source: `cmd/hopboxd/main.go`
- In-container CLI source: `cmd/hopbox/main.go`
- Templates: `templates/Dockerfile.base`, `templates/hopbox` (built by `scripts/build-cli.sh`)
- Config template: `config.example.toml`
- Existing deploy files: `deploy/hopboxd.service`, `deploy/monitoring/{compose.yml,prometheus.yml,README.md}`, `deploy/grafana/{server-overview.json,box-details.json,README.md}`
- No `.github/` directory exists yet

Release tarball layout (from spec, Task 1 and Task 3 must produce exactly this):

```
hopbox-<version>/
├── hopboxd
├── templates/
│   ├── Dockerfile.base
│   └── hopbox
├── config.example.toml
├── deploy/
│   ├── hopboxd.service
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
└── VERSION
```

---

## Task 1: Build release script

**File:** `scripts/build-release.sh` (new)

Builds a release tarball for a single OS/arch. Must work locally and in CI. Called as:

```bash
./scripts/build-release.sh v0.1.0 linux amd64
./scripts/build-release.sh v0.1.0 linux arm64
```

Produces `dist/hopbox-<version>-<os>-<arch>.tar.gz`.

### Implementation

```bash
#!/usr/bin/env bash
# scripts/build-release.sh — Build a hopbox release tarball.
#
# Usage: scripts/build-release.sh <version> <os> <arch>
# Example: scripts/build-release.sh v0.1.0 linux amd64
#
# Produces: dist/hopbox-<version>-<os>-<arch>.tar.gz
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "Usage: $0 <version> <os> <arch>" >&2
  echo "Example: $0 v0.1.0 linux amd64" >&2
  exit 1
fi

VERSION="$1"
OS="$2"
ARCH="$3"

# Resolve repo root (script lives in scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

DIST_DIR="${REPO_ROOT}/dist"
STAGE_DIR="${DIST_DIR}/hopbox-${VERSION}"
TARBALL="${DIST_DIR}/hopbox-${VERSION}-${OS}-${ARCH}.tar.gz"

echo "==> Building hopbox ${VERSION} for ${OS}/${ARCH}"

# Clean previous staging for this version
rm -rf "${STAGE_DIR}"
mkdir -p "${STAGE_DIR}/templates" "${STAGE_DIR}/deploy"

# Build hopboxd
echo "    building hopboxd..."
GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${STAGE_DIR}/hopboxd" ./cmd/hopboxd

# Build in-container hopbox CLI
echo "    building hopbox CLI..."
GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${STAGE_DIR}/templates/hopbox" ./cmd/hopbox

# Copy templates
cp templates/Dockerfile.base "${STAGE_DIR}/templates/Dockerfile.base"

# Copy top-level config example
cp config.example.toml "${STAGE_DIR}/config.example.toml"

# Copy systemd unit
cp deploy/hopboxd.service "${STAGE_DIR}/deploy/hopboxd.service"

# Copy monitoring stack (entire directory — compose, prometheus, grafana provisioning and dashboards)
mkdir -p "${STAGE_DIR}/deploy/monitoring"
cp -R deploy/monitoring/. "${STAGE_DIR}/deploy/monitoring/"

# Strip README from monitoring dir (not needed in release)
rm -f "${STAGE_DIR}/deploy/monitoring/README.md"

# VERSION file
echo "${VERSION}" > "${STAGE_DIR}/VERSION"

# Create tarball
echo "    creating tarball..."
rm -f "${TARBALL}"
tar -C "${DIST_DIR}" -czf "${TARBALL}" "hopbox-${VERSION}"

echo "==> Wrote ${TARBALL}"

# Print contents summary
echo
echo "Tarball contents:"
tar -tzf "${TARBALL}" | sed 's/^/  /'
```

### Acceptance

- Script is executable (`chmod +x scripts/build-release.sh`).
- Running `./scripts/build-release.sh v0.0.1-test linux $(go env GOARCH)` from the repo root succeeds and produces a tarball whose contents match the layout above.
- `hopboxd` and `templates/hopbox` binaries are present and built for the requested OS/arch.
- `deploy/monitoring/grafana/provisioning/{datasources,dashboards}/*.yml` and `deploy/monitoring/grafana/dashboards/*.json` are present inside the tarball (this depends on Task 2 being done first).

---

## Task 2: Grafana provisioning files

**New files:**

### `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml`

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

### `deploy/monitoring/grafana/provisioning/dashboards/hopbox.yml`

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

### `deploy/monitoring/grafana/dashboards/server-overview.json`

Exact copy of `deploy/grafana/server-overview.json`. Do not hand-edit.

```bash
cp deploy/grafana/server-overview.json deploy/monitoring/grafana/dashboards/server-overview.json
```

### `deploy/monitoring/grafana/dashboards/box-details.json`

Exact copy of `deploy/grafana/box-details.json`.

```bash
cp deploy/grafana/box-details.json deploy/monitoring/grafana/dashboards/box-details.json
```

### Modified: `deploy/monitoring/compose.yml`

Update the grafana service to mount the provisioning and dashboards directories. The full updated file:

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    container_name: hopbox-prometheus
    restart: unless-stopped
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    extra_hosts:
      - "host.docker.internal:host-gateway"

  grafana:
    image: grafana/grafana:latest
    container_name: hopbox-grafana
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning
      - ./grafana/dashboards:/var/lib/grafana/dashboards
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

### Acceptance

- All four files exist under `deploy/monitoring/grafana/`.
- `deploy/monitoring/compose.yml` mounts both `./grafana/provisioning` and `./grafana/dashboards`.
- Dashboard JSON files byte-match their siblings in `deploy/grafana/` (`diff -q` returns no difference).

---

## Task 3: GitHub Actions release workflow

**File:** `.github/workflows/release.yml` (new — `.github/` and `.github/workflows/` may not exist yet)

Triggered on tag push matching `v*`. Matrix builds amd64 and arm64, uploads tarballs as artifacts, then a final job downloads all artifacts and publishes a GitHub release with both tarballs attached.

### Implementation

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  build:
    name: Build ${{ matrix.arch }}
    runs-on: ubuntu-latest
    strategy:
      fail-fast: true
      matrix:
        arch: [amd64, arm64]
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          check-latest: true

      - name: Resolve version from tag
        id: version
        run: echo "version=${GITHUB_REF_NAME}" >> "$GITHUB_OUTPUT"

      - name: Build release tarball
        run: |
          chmod +x scripts/build-release.sh
          ./scripts/build-release.sh "${{ steps.version.outputs.version }}" linux ${{ matrix.arch }}

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: hopbox-${{ steps.version.outputs.version }}-linux-${{ matrix.arch }}
          path: dist/hopbox-${{ steps.version.outputs.version }}-linux-${{ matrix.arch }}.tar.gz
          if-no-files-found: error
          retention-days: 7

  release:
    name: Publish GitHub Release
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Download all artifacts
        uses: actions/download-artifact@v4
        with:
          path: artifacts

      - name: Flatten artifacts
        run: |
          set -euo pipefail
          mkdir -p dist
          find artifacts -type f -name '*.tar.gz' -exec cp {} dist/ \;
          ls -la dist/

      - name: Create GitHub release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/*.tar.gz
          generate_release_notes: true
          fail_on_unmatched_files: true
```

### Acceptance

- Workflow file is valid YAML.
- Uses `actions/checkout@v4`, `actions/setup-go@v5` (Go 1.24), `actions/upload-artifact@v4`, `actions/download-artifact@v4`, `softprops/action-gh-release@v2` exactly.
- Triggered only on tags matching `v*`.
- Matrix covers amd64 and arm64.
- Release job depends on build job and attaches both tarballs.

---

## Task 4: Install script

**File:** `scripts/install.sh` (new)

Bash script that downloads and installs a hopbox release. Works for fresh installs and upgrades. Must be runnable via `curl | sudo bash`.

### Contract

Arguments (all optional, order-independent):
- Positional: version tag like `v0.1.0`. If omitted, fetches latest from GitHub API.
- Flag: `--with-monitoring` — also sets up the Prometheus + Grafana stack.

### Implementation

```bash
#!/usr/bin/env bash
# scripts/install.sh — Install or upgrade hopbox from a GitHub release.
#
# Usage:
#   sudo ./install.sh                              # latest
#   sudo ./install.sh v0.1.0                       # specific version
#   sudo ./install.sh --with-monitoring            # latest + monitoring
#   sudo ./install.sh v0.1.0 --with-monitoring     # both
#
# Or via curl:
#   curl -sSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh | sudo bash
#   curl -sSL .../install.sh | sudo bash -s -- v0.1.0 --with-monitoring
set -euo pipefail

# ---------- colors ----------
if [[ -t 1 ]]; then
  RED=$'\033[0;31m'
  GREEN=$'\033[0;32m'
  YELLOW=$'\033[0;33m'
  BLUE=$'\033[0;34m'
  BOLD=$'\033[1m'
  RESET=$'\033[0m'
else
  RED="" GREEN="" YELLOW="" BLUE="" BOLD="" RESET=""
fi

info()  { printf '%s==>%s %s\n' "${BLUE}" "${RESET}" "$*"; }
ok()    { printf '%s✓%s %s\n'   "${GREEN}" "${RESET}" "$*"; }
warn()  { printf '%s!%s %s\n'   "${YELLOW}" "${RESET}" "$*" >&2; }
err()   { printf '%s✗%s %s\n'   "${RED}" "${RESET}" "$*" >&2; }
die()   { err "$*"; exit 1; }

# ---------- parse args ----------
VERSION=""
WITH_MONITORING=0
for arg in "$@"; do
  case "$arg" in
    --with-monitoring) WITH_MONITORING=1 ;;
    v*) VERSION="$arg" ;;
    -h|--help)
      sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) die "unknown argument: $arg" ;;
  esac
done

# ---------- preflight ----------
info "Running preflight checks"

[[ $EUID -eq 0 ]] || die "must run as root (use sudo)"

[[ "$(uname -s)" == "Linux" ]] || die "hopbox requires Linux (detected: $(uname -s))"

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac
ok "platform: linux/${ARCH}"

command -v docker >/dev/null 2>&1 \
  || die "Docker is required but not installed. Install Docker first: https://docs.docker.com/engine/install/"
ok "docker: $(docker --version)"

if [[ $WITH_MONITORING -eq 1 ]]; then
  if ! docker compose version >/dev/null 2>&1; then
    die "'docker compose' plugin is required for --with-monitoring but was not found"
  fi
  ok "docker compose: $(docker compose version | head -n1)"
fi

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar  >/dev/null 2>&1 || die "tar is required"

# ---------- resolve version ----------
if [[ -z "$VERSION" ]]; then
  info "Resolving latest release from GitHub"
  API_URL="https://api.github.com/repos/hopboxdev/hopbox/releases/latest"
  VERSION="$(curl -fsSL "$API_URL" | grep -oE '"tag_name":\s*"[^"]+"' | head -n1 | sed -E 's/.*"([^"]+)".*/\1/')"
  [[ -n "$VERSION" ]] || die "could not determine latest version from $API_URL"
fi
ok "version: ${BOLD}${VERSION}${RESET}"

# ---------- download + extract ----------
TARBALL_NAME="hopbox-${VERSION}-linux-${ARCH}.tar.gz"
TARBALL_URL="https://github.com/hopboxdev/hopbox/releases/download/${VERSION}/${TARBALL_NAME}"
TMP_TARBALL="/tmp/${TARBALL_NAME}"
INSTALL_ROOT="/opt/hopbox"
VERSION_DIR="${INSTALL_ROOT}/${VERSION}"

info "Downloading ${TARBALL_URL}"
rm -f "$TMP_TARBALL"
curl -fSL --progress-bar "$TARBALL_URL" -o "$TMP_TARBALL" \
  || die "download failed: $TARBALL_URL"

info "Extracting to ${INSTALL_ROOT}"
mkdir -p "$INSTALL_ROOT"
# Tarball top-level dir is hopbox-<version>/; rename to <version>/ after extract.
rm -rf "$VERSION_DIR"
tar -C "$INSTALL_ROOT" -xzf "$TMP_TARBALL"
mv "${INSTALL_ROOT}/hopbox-${VERSION}" "$VERSION_DIR"
rm -f "$TMP_TARBALL"
ok "extracted to ${VERSION_DIR}"

# ---------- user + dirs ----------
info "Ensuring hopbox system user and directories"
if ! id -u hopbox >/dev/null 2>&1; then
  useradd -r -s /usr/sbin/nologin -d /opt/hopbox hopbox
  ok "created hopbox user"
else
  ok "hopbox user exists"
fi

if getent group docker >/dev/null 2>&1; then
  usermod -aG docker hopbox
  ok "hopbox added to docker group"
else
  warn "docker group missing; skipping group membership"
fi

mkdir -p /etc/hopbox /var/lib/hopbox
chown -R hopbox:hopbox /var/lib/hopbox "$VERSION_DIR"

# ---------- config ----------
if [[ ! -f /etc/hopbox/config.toml ]]; then
  cp "${VERSION_DIR}/config.example.toml" /etc/hopbox/config.toml
  chown hopbox:hopbox /etc/hopbox/config.toml
  chmod 640 /etc/hopbox/config.toml
  ok "installed /etc/hopbox/config.toml from example"
else
  ok "/etc/hopbox/config.toml already exists (preserved)"
fi

# ---------- systemd ----------
info "Installing systemd unit"
install -m 0644 "${VERSION_DIR}/deploy/hopboxd.service" /etc/systemd/system/hopboxd.service
systemctl daemon-reload
ok "systemd unit installed"

# ---------- symlinks ----------
info "Updating symlinks"
ln -sfn "$VERSION_DIR" "${INSTALL_ROOT}/current"
ln -sfn "${INSTALL_ROOT}/current/hopboxd" /usr/local/bin/hopboxd
ok "current -> ${VERSION_DIR}"
ok "/usr/local/bin/hopboxd -> /opt/hopbox/current/hopboxd"

# ---------- enable + (re)start ----------
info "Enabling and starting hopboxd"
systemctl enable hopboxd >/dev/null 2>&1 || true
if systemctl is-active --quiet hopboxd; then
  systemctl restart hopboxd
  ok "hopboxd restarted"
else
  systemctl start hopboxd
  ok "hopboxd started"
fi

sleep 1
if systemctl is-active --quiet hopboxd; then
  ok "hopboxd is running"
else
  warn "hopboxd is not active — check: journalctl -u hopboxd -n 50"
fi

# ---------- monitoring (optional) ----------
if [[ $WITH_MONITORING -eq 1 ]]; then
  info "Installing monitoring stack"
  MON_DIR="${INSTALL_ROOT}/monitoring"
  if [[ ! -d "$MON_DIR" ]]; then
    cp -R "${VERSION_DIR}/deploy/monitoring" "$MON_DIR"
    ok "copied monitoring stack to ${MON_DIR}"
  else
    ok "${MON_DIR} already exists (preserving prometheus.yml and grafana config)"
  fi

  (cd "$MON_DIR" && docker compose up -d)
  ok "monitoring stack is up"
  echo
  echo "  Grafana:    http://<server>:3000  (admin / admin — change on first login)"
  echo "  Prometheus: http://<server>:9090"
fi

# ---------- summary ----------
cat <<EOF

${GREEN}${BOLD}Hopbox ${VERSION} installed successfully.${RESET}

  Config:  /etc/hopbox/config.toml
  Data:    /var/lib/hopbox/
  Binary:  /usr/local/bin/hopboxd
  Service: systemctl status hopboxd

Next steps:
  1. Edit /etc/hopbox/config.toml (set hostname, admin password, etc.)
  2. Restart:  systemctl restart hopboxd
  3. Connect:  ssh -p 2222 hop@your-server

EOF
```

### Acceptance

- Script uses `set -euo pipefail`, quotes all variable expansions that could contain spaces.
- Fails clearly if: not root, not Linux, unsupported arch, docker missing, `--with-monitoring` requested without docker compose.
- Version resolution works with and without explicit argument (latest via GitHub API).
- Fresh install: creates user, dirs, config, systemd unit, symlinks, starts service.
- Upgrade (same script, new version): updates symlink atomically via `ln -sfn`, preserves existing `/etc/hopbox/config.toml`, restarts service.
- `--with-monitoring` copies monitoring dir only if absent, runs `docker compose up -d`.
- Color output: green for success, yellow for warnings, red for errors.
- `chmod +x scripts/install.sh` so it's executable in the repo.

---

## Task 5: Local test of build-release.sh

Manual verification that Tasks 1 and 2 work end-to-end before pushing.

### Steps

```bash
cd /Users/gandalfledev/Developer/hopbox

# Build for the local machine's arch (mac builds a linux binary via cross-compile — that's fine, CGO is off)
./scripts/build-release.sh v0.0.1-test linux $(go env GOARCH)

# Inspect tarball structure
tar -tzf dist/hopbox-v0.0.1-test-linux-*.tar.gz | sort
```

### Expected tarball entries (at minimum)

```
hopbox-v0.0.1-test/
hopbox-v0.0.1-test/VERSION
hopbox-v0.0.1-test/config.example.toml
hopbox-v0.0.1-test/deploy/
hopbox-v0.0.1-test/deploy/hopboxd.service
hopbox-v0.0.1-test/deploy/monitoring/
hopbox-v0.0.1-test/deploy/monitoring/compose.yml
hopbox-v0.0.1-test/deploy/monitoring/prometheus.yml
hopbox-v0.0.1-test/deploy/monitoring/grafana/
hopbox-v0.0.1-test/deploy/monitoring/grafana/dashboards/
hopbox-v0.0.1-test/deploy/monitoring/grafana/dashboards/box-details.json
hopbox-v0.0.1-test/deploy/monitoring/grafana/dashboards/server-overview.json
hopbox-v0.0.1-test/deploy/monitoring/grafana/provisioning/
hopbox-v0.0.1-test/deploy/monitoring/grafana/provisioning/dashboards/
hopbox-v0.0.1-test/deploy/monitoring/grafana/provisioning/dashboards/hopbox.yml
hopbox-v0.0.1-test/deploy/monitoring/grafana/provisioning/datasources/
hopbox-v0.0.1-test/deploy/monitoring/grafana/provisioning/datasources/prometheus.yml
hopbox-v0.0.1-test/hopboxd
hopbox-v0.0.1-test/templates/
hopbox-v0.0.1-test/templates/Dockerfile.base
hopbox-v0.0.1-test/templates/hopbox
```

### Acceptance

- Tarball exists at `dist/hopbox-v0.0.1-test-linux-<arch>.tar.gz`.
- All paths above are present.
- `file dist/.../hopbox-v0.0.1-test/hopboxd` reports a Linux ELF binary for the requested arch (extract one to check: `tar -xzf dist/hopbox-v0.0.1-test-linux-*.tar.gz -C /tmp && file /tmp/hopbox-v0.0.1-test/hopboxd`).
- `cat /tmp/hopbox-v0.0.1-test/VERSION` prints `v0.0.1-test`.
- `dist/` is gitignored or cleaned up afterwards (add `dist/` to `.gitignore` if not already present).

---

## Out of scope (per spec)

- Automatic Docker installation
- Firewall rule management
- HTTPS / cert management for admin UI
- Auto-rollback on failure
- Release note curation beyond GitHub's auto-generated commit log
- Checksum or signature verification of downloaded tarballs

---

## Completion checklist

- [ ] `scripts/build-release.sh` exists and is executable
- [ ] `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml` exists
- [ ] `deploy/monitoring/grafana/provisioning/dashboards/hopbox.yml` exists
- [ ] `deploy/monitoring/grafana/dashboards/server-overview.json` matches `deploy/grafana/server-overview.json`
- [ ] `deploy/monitoring/grafana/dashboards/box-details.json` matches `deploy/grafana/box-details.json`
- [ ] `deploy/monitoring/compose.yml` mounts both grafana directories
- [ ] `.github/workflows/release.yml` exists and is valid YAML
- [ ] `scripts/install.sh` exists and is executable
- [ ] `dist/` is in `.gitignore`
- [ ] Task 5 local smoke test passes
