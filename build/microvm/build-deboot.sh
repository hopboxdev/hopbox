#!/usr/bin/env bash
# build-deboot.sh — build a debootstrap-based catalog image (Debian or Ubuntu).
# Unlike the prebuilt Firecracker CI ubuntu rootfs (build-rootfs.sh), a
# debootstrapped image is a full system with a dpkg database, so the dev tools
# install cleanly and the box stays apt-extensible.
#
# Run on Linux as root. Needs debootstrap + mkfs.ext4 (auto-installed when apt is
# present). Binaries are cross-compiled if `go` is present, else supplied via
# $AGENT_BIN / $GUEST_BIN.
#
# Env: DISTRO=debian|ubuntu (default debian), plus OUT_DIR, IMAGE, SUITE, MIRROR,
#      KEYRING, SIZE_MB, PACKAGES, INIT_ASSET.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-/opt/hopbox-microvm}"
DISTRO="${DISTRO:-debian}"
case "$DISTRO" in
  debian) d_suite=bookworm; d_image=debian-12;    d_mirror=http://deb.debian.org/debian;    d_keyring=/usr/share/keyrings/debian-archive-keyring.gpg; d_generic=sid;    d_keypkg=debian-archive-keyring ;;
  ubuntu) d_suite=jammy;    d_image=ubuntu-22.04; d_mirror=http://archive.ubuntu.com/ubuntu; d_keyring=/usr/share/keyrings/ubuntu-archive-keyring.gpg; d_generic=gutsy; d_keypkg=ubuntu-keyring ;;
  *) echo "unknown DISTRO=$DISTRO (want debian|ubuntu)"; exit 1 ;;
esac
SUITE="${SUITE:-$d_suite}"
IMAGE="${IMAGE:-$d_image}"
MIRROR="${MIRROR:-$d_mirror}"
KEYRING="${KEYRING:-$d_keyring}"
SIZE_MB="${SIZE_MB:-1536}" # headroom for the dev tools below
# Dev tools baked in so a box is usable out of the box (mirrors build-rootfs.sh).
PACKAGES="${PACKAGES:-git vim nano curl wget less ca-certificates openssh-client tmux htop python3}"
INIT_ASSET="${INIT_ASSET:-$REPO_ROOT/providers/compute/microvm/assets/hopbox-init}"

[ "$(id -u)" = 0 ] || { echo "must run as root"; exit 1; }

echo "==> ensure build deps (debootstrap + $DISTRO keyring)"
need=""
command -v debootstrap >/dev/null || need="debootstrap"
[ -f "$KEYRING" ] || need="$need $d_keypkg"
if [ -n "$need" ] && command -v apt-get >/dev/null; then
  apt-get update -qq && apt-get install -y -qq $need || true
fi
command -v debootstrap >/dev/null || { echo "need debootstrap"; exit 1; }
# debootstrap may lack a script for a newer suite name; fall back to the distro's
# generic script (gutsy for ubuntu, sid for debian).
SCRIPTS=/usr/share/debootstrap/scripts
[ -e "$SCRIPTS/$SUITE" ] || ln -sf "$d_generic" "$SCRIPTS/$SUITE"
# Verify the release with the keyring if present, else build unverified (a
# self-hosted build on a trusted network).
gpg_arg="--keyring=$KEYRING"
[ -f "$KEYRING" ] || { gpg_arg="--no-check-gpg"; echo "!! no $KEYRING — building unverified (--no-check-gpg)"; }

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

echo "==> debootstrap $DISTRO $SUITE (minbase + dev tools)"
inc="bash,ca-certificates"
[ -n "$PACKAGES" ] && inc="$inc,$(echo "$PACKAGES" | tr ' ' ',')"
debootstrap --variant=minbase $gpg_arg --include="$inc" "$SUITE" "$ROOT" "$MIRROR" >/dev/null

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
