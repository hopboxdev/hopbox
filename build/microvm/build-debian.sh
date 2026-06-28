#!/usr/bin/env bash
# build-debian.sh — build a Debian image for the microVM catalog via debootstrap
# (the Firecracker CI only ships an Ubuntu ext4, so other distros are bootstrapped).
#
# Run on Linux as root. Needs debootstrap + mkfs.ext4. Binaries are cross-compiled
# if `go` is present, else supplied via $AGENT_BIN / $GUEST_BIN.
#
# Env: OUT_DIR (default /opt/hopbox-microvm), IMAGE (default debian-default),
#      SUITE (default bookworm), SIZE_MB (default 1024), MIRROR, INIT_ASSET.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-/opt/hopbox-microvm}"
IMAGE="${IMAGE:-debian-12}"
SUITE="${SUITE:-bookworm}"
SIZE_MB="${SIZE_MB:-1536}" # headroom for the dev tools below
MIRROR="${MIRROR:-http://deb.debian.org/debian}"
# Dev tools baked in so a box is usable out of the box (mirrors build-rootfs.sh).
PACKAGES="${PACKAGES:-git vim nano curl wget less ca-certificates openssh-client tmux htop python3}"
INIT_ASSET="${INIT_ASSET:-$REPO_ROOT/providers/compute/microvm/assets/hopbox-init}"

[ "$(id -u)" = 0 ] || { echo "must run as root"; exit 1; }
command -v debootstrap >/dev/null || { echo "need debootstrap (apt-get install debootstrap)"; exit 1; }

WORK="$(mktemp -d)"; ROOT="$WORK/root"; MNT="$WORK/mnt"; mkdir -p "$ROOT" "$MNT"
trap 'umount "$MNT" 2>/dev/null || true; rm -rf "$WORK"' EXIT

echo "==> binaries"
if [ -n "${AGENT_BIN:-}" ] && [ -n "${GUEST_BIN:-}" ]; then
  cp "$AGENT_BIN" "$WORK/hopbox-agent"; cp "$GUEST_BIN" "$WORK/box-guest"
elif command -v go >/dev/null; then
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$WORK/hopbox-agent" ./cmd/hopbox-agent )
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$WORK/box-guest"    ./cmd/box-guest )
else
  echo "no go and no \$AGENT_BIN/\$GUEST_BIN"; exit 1
fi

echo "==> debootstrap $SUITE (minbase + bash + dev tools)"
inc="bash,ca-certificates"
[ -n "$PACKAGES" ] && inc="$inc,$(echo "$PACKAGES" | tr ' ' ',')"
debootstrap --variant=minbase --include="$inc" "$SUITE" "$ROOT" "$MIRROR" >/dev/null

echo "==> compose image '$IMAGE' (${SIZE_MB}MB ext4)"
mkdir -p "$OUT_DIR/images"
IMG="$OUT_DIR/images/$IMAGE.ext4"
truncate -s "${SIZE_MB}M" "$IMG"
mkfs.ext4 -q -F "$IMG"
mount -o loop "$IMG" "$MNT"
cp -a "$ROOT/." "$MNT/"
install -m0755 "$WORK/hopbox-agent" "$MNT/usr/local/bin/hopbox-agent"
install -m0755 "$WORK/box-guest"    "$MNT/usr/local/bin/box-guest"
install -m0755 "$INIT_ASSET"        "$MNT/sbin/hopbox-init"
sync; umount "$MNT"

echo "==> done: image $IMG ($(du -h "$IMG" | cut -f1))"
