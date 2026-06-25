#!/bin/sh
# Hopbox CLI installer (macOS + Linux).
#
#   curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install-cli.sh | sh
#
# Installs only the `hopbox` client CLI — the tool you run on your laptop to
# create workspaces and ssh into them. The server components (hopboxd, hopbox-gw,
# hopbox-agent) are Linux + Docker; deploy those with deploy/install.sh.
#
# Environment overrides:
#   HOPBOX_VERSION   release tag to install (default: latest)
#   HOPBOX_BINDIR    install directory (default: /usr/local/bin if writable, else ~/.local/bin)
set -eu

REPO="hopboxdev/hopbox"
VERSION="${HOPBOX_VERSION:-latest}"

log() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *) die "unsupported OS $(uname -s) (macOS and Linux only)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported arch $(uname -m)" ;;
esac
log "platform: $OS/$ARCH"

# --- pick an install dir (no sudo if we can avoid it) ---
if [ -n "${HOPBOX_BINDIR:-}" ]; then
  BINDIR="$HOPBOX_BINDIR"
elif [ -w /usr/local/bin ]; then
  BINDIR=/usr/local/bin
else
  BINDIR="$HOME/.local/bin"
fi
mkdir -p "$BINDIR"

# --- download the release asset for this platform ---
base="https://github.com/$REPO/releases"
if [ "$VERSION" = "latest" ]; then
  dl="$base/latest/download"
else
  dl="$base/download/$VERSION"
fi
asset="hopbox_${OS}_${ARCH}.tar.gz"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
log "downloading $asset"
curl -fsSL -o "$tmp/cli.tgz" "$dl/$asset" \
  || die "download failed: $dl/$asset (is there a release with $OS/$ARCH assets?)"
tar -xzf "$tmp/cli.tgz" -C "$tmp" hopbox || die "archive missing hopbox binary"

install -m755 "$tmp/hopbox" "$BINDIR/hopbox"
log "installed hopbox to $BINDIR/hopbox"

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) log "note: $BINDIR is not on your PATH — add it, e.g. export PATH=\"$BINDIR:\$PATH\"" ;;
esac

"$BINDIR/hopbox" --help >/dev/null 2>&1 && log "done — run: hopbox --help"
