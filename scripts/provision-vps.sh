#!/usr/bin/env bash
# scripts/provision-vps.sh — One-shot VPS provisioning for hopbox.
#
# What it does, in order:
#   1. Preflight: root, Linux, Debian/Ubuntu, internet reachable
#   2. Updates apt and installs curl/tar/ufw if missing
#   3. Installs Docker Engine + compose plugin (via get.docker.com)
#   4. Configures UFW: deny incoming, allow 22/tcp (admin SSH) and 2222/tcp (hopbox SSH)
#   5. Runs scripts/install.sh to download + install hopbox from GitHub releases
#   6. Prints a summary with next steps
#
# What it intentionally does NOT do (too easy to lock yourself out):
#   - Create admin users
#   - Disable root/password SSH login
#   - Configure DNS
#   - Open ports for the admin UI or Grafana (those stay localhost-only; tunnel via SSH)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/provision-vps.sh | sudo bash
#   curl -fsSL .../provision-vps.sh | sudo bash -s -- --with-monitoring
#   curl -fsSL .../provision-vps.sh | sudo bash -s -- v0.1.0 --with-monitoring
#
# Flags:
#   v<X.Y.Z>            Pin a specific hopbox version (default: latest release)
#   --with-monitoring   Also start the Prometheus + Grafana stack
#   --skip-docker       Assume Docker is already installed correctly
#   --skip-firewall     Do not touch UFW
#   -h, --help          Show this help

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

info()  { printf '%s==>%s %s\n' "${BLUE}"  "${RESET}" "$*"; }
ok()    { printf '%s✓%s %s\n'   "${GREEN}" "${RESET}" "$*"; }
warn()  { printf '%s!%s %s\n'   "${YELLOW}" "${RESET}" "$*" >&2; }
err()   { printf '%s✗%s %s\n'   "${RED}"   "${RESET}" "$*" >&2; }
die()   { err "$*"; exit 1; }

# ---------- parse args ----------
VERSION=""
WITH_MONITORING=0
SKIP_DOCKER=0
SKIP_FIREWALL=0
PASSTHROUGH=()

for arg in "$@"; do
  case "$arg" in
    --with-monitoring)
      WITH_MONITORING=1
      PASSTHROUGH+=("--with-monitoring")
      ;;
    --skip-docker)   SKIP_DOCKER=1 ;;
    --skip-firewall) SKIP_FIREWALL=1 ;;
    v*)
      VERSION="$arg"
      PASSTHROUGH+=("$arg")
      ;;
    -h|--help)
      sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) die "unknown argument: $arg (try --help)" ;;
  esac
done

# ---------- preflight ----------
info "Running preflight checks"

[[ $EUID -eq 0 ]] || die "must run as root (use sudo)"
[[ "$(uname -s)" == "Linux" ]] || die "requires Linux (detected: $(uname -s))"

if [[ ! -f /etc/os-release ]]; then
  die "cannot detect distro (/etc/os-release missing)"
fi
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}" in
  ubuntu|debian) ok "distro: ${PRETTY_NAME}" ;;
  *) warn "distro '${ID:-unknown}' is untested; proceeding anyway" ;;
esac

if ! curl -fsS --max-time 5 https://github.com >/dev/null 2>&1; then
  die "cannot reach github.com — check network/DNS before proceeding"
fi
ok "internet reachable"

# ---------- apt basics ----------
info "Updating apt and installing base packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y >/dev/null
apt-get install -y curl ca-certificates tar ufw >/dev/null
ok "base packages installed (curl, ca-certificates, tar, ufw)"

# ---------- docker ----------
if [[ $SKIP_DOCKER -eq 1 ]]; then
  ok "skipping Docker install (--skip-docker)"
  command -v docker >/dev/null || die "--skip-docker set but 'docker' not on PATH"
elif command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  ok "docker already installed: $(docker --version)"
else
  info "Installing Docker Engine via get.docker.com"
  curl -fsSL https://get.docker.com | sh >/dev/null
  systemctl enable --now docker >/dev/null 2>&1 || true
  ok "docker installed: $(docker --version)"
fi

if ! docker compose version >/dev/null 2>&1; then
  warn "'docker compose' plugin not available — --with-monitoring will fail"
fi

# ---------- firewall ----------
if [[ $SKIP_FIREWALL -eq 1 ]]; then
  ok "skipping firewall config (--skip-firewall)"
else
  info "Configuring UFW firewall"
  # Always allow the current admin SSH session first to avoid lockout.
  ufw --force default deny incoming >/dev/null
  ufw --force default allow outgoing >/dev/null
  ufw allow 22/tcp   >/dev/null
  ufw allow 2222/tcp >/dev/null
  # Enable non-interactively. On first enable UFW warns about SSH; we've already allowed 22.
  if ufw status | grep -q "Status: active"; then
    ok "ufw already active"
  else
    yes | ufw enable >/dev/null
    ok "ufw enabled"
  fi
  ufw reload >/dev/null
  echo
  ufw status verbose | sed 's/^/    /'
  echo
fi

# ---------- hopbox ----------
info "Installing hopbox"
INSTALL_URL="https://raw.githubusercontent.com/hopboxdev/hopbox/main/scripts/install.sh"

if [[ ${#PASSTHROUGH[@]} -gt 0 ]]; then
  curl -fsSL "$INSTALL_URL" | bash -s -- "${PASSTHROUGH[@]}"
else
  curl -fsSL "$INSTALL_URL" | bash
fi

# ---------- summary ----------
PUBLIC_IP="$(curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null || echo '<server>')"

cat <<EOF

${GREEN}${BOLD}VPS provisioning complete.${RESET}

  Public IP:  ${PUBLIC_IP}
  Hopbox:     systemctl status hopboxd
  Logs:       journalctl -u hopboxd -f
  Config:     /etc/hopbox/config.toml

${BOLD}Things to do BEFORE opening this to users:${RESET}
  1. Edit /etc/hopbox/config.toml:
       - set hostname to your domain or public IP
       - set a strong admin.password
       - review resources.cpu_cores / memory_gb
  2. Restart: systemctl restart hopboxd
  3. Harden OpenSSH on :22 (disable root login, disable password auth).
     This script did NOT touch /etc/ssh/sshd_config to avoid lockouts.
  4. Tunnel the admin UI from your laptop:
       ssh -L 8080:localhost:8080 <you>@${PUBLIC_IP}
       # then open http://localhost:8080

${BOLD}First hopbox connection:${RESET}
  ssh -p 2222 hop@${PUBLIC_IP}

EOF
