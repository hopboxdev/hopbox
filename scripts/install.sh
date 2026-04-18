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

# ---------- devcontainer images ----------
info "Building devcontainer base and builder images"
docker build -q -t "ghcr.io/hopboxdev/devcontainer-base:${VERSION}" "${VERSION_DIR}/templates/base-devcontainer/" >/dev/null \
  || die "failed to build devcontainer-base image"
docker tag "ghcr.io/hopboxdev/devcontainer-base:${VERSION}" ghcr.io/hopboxdev/devcontainer-base:dev
ok "built ghcr.io/hopboxdev/devcontainer-base:${VERSION} (also tagged :dev)"

docker build -q -t "ghcr.io/hopboxdev/builder:${VERSION}" "${VERSION_DIR}/templates/builder/" >/dev/null \
  || die "failed to build builder image"
docker tag "ghcr.io/hopboxdev/builder:${VERSION}" ghcr.io/hopboxdev/builder:dev
ok "built ghcr.io/hopboxdev/builder:${VERSION} (also tagged :dev)"

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

# ---------- monitoring ----------
MON_DIR="${INSTALL_ROOT}/monitoring"

# Always refresh dashboards and provisioning if monitoring is already installed.
# compose.yml and prometheus.yml are user config — never overwrite them.
if [[ -d "$MON_DIR" ]]; then
  rm -rf "$MON_DIR/grafana/provisioning"
  cp -R "${VERSION_DIR}/deploy/monitoring/grafana/provisioning" "$MON_DIR/grafana/provisioning"
  rm -rf "$MON_DIR/grafana/dashboards"
  cp -R "${VERSION_DIR}/deploy/monitoring/grafana/dashboards" "$MON_DIR/grafana/dashboards"
  ok "refreshed monitoring dashboards and provisioning"

  (cd "$MON_DIR" && docker compose up -d)
  ok "monitoring stack is up"
fi

# First-time monitoring install (only with --with-monitoring)
if [[ $WITH_MONITORING -eq 1 ]] && [[ ! -d "$MON_DIR" ]]; then
  info "Installing monitoring stack"
  cp -R "${VERSION_DIR}/deploy/monitoring" "$MON_DIR"
  ok "copied monitoring stack to ${MON_DIR}"

  (cd "$MON_DIR" && docker compose up -d)
  ok "monitoring stack is up"
  echo
  echo "  Grafana:    http://127.0.0.1:3000  (admin / admin — change on first login)"
  echo "  Prometheus: http://127.0.0.1:9090"
  echo "  (both bound to localhost — tunnel over SSH or front with a reverse proxy)"
fi

# ---------- summary ----------
cat <<EOF

${GREEN}${BOLD}Hopbox ${VERSION} installed successfully.${RESET}

  Config:  /etc/hopbox/config.toml
  Data:    /var/lib/hopbox/
  Binary:  /usr/local/bin/hopboxd
  Service: systemctl status hopboxd

Next steps:
  1. Edit /etc/hopbox/config.toml
       - set hostname, admin.username, admin.password
       - generate a strong password: openssl rand -base64 24
  2. Restart:  systemctl restart hopboxd
  3. Connect:  ssh -p 2222 hop@your-server

EOF
