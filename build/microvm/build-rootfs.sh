#!/usr/bin/env bash
# build-rootfs.sh — build the golden microVM agent rootfs (F6).
#
# Produces a reproducible agent rootfs (+ kernel) for the Firecracker provider:
# a pinned base ext4 with hopbox-agent, box-guest, and the in-VM init baked in.
# The provider boots each box from a CoW clone of this image.
#
# Run on Linux as root (it loop-mounts the image). The agent + box-guest binaries
# are cross-compiled here if `go` is available, or supplied pre-built via
# $AGENT_BIN and $GUEST_BIN (e.g. built on another host and copied over).
#
# Outputs $OUT_DIR/{vmlinux, agent.ext4}. Env (all optional):
#   OUT_DIR     output dir                (default /opt/hopbox-microvm)
#   CACHE_DIR   download cache            (default $OUT_DIR/cache)
#   BASE_URL    base rootfs ext4          (pinned Firecracker CI ubuntu 22.04)
#   KERNEL_URL  vmlinux                   (pinned Firecracker CI 5.10)
#   GROW_MB     headroom added to base    (default 128)
#   AGENT_BIN   prebuilt linux agent      (else cross-compiled)
#   GUEST_BIN   prebuilt linux box-guest  (else cross-compiled)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-/opt/hopbox-microvm}"
CACHE_DIR="${CACHE_DIR:-$OUT_DIR/cache}"
IMAGE="${IMAGE:-ubuntu-22.04}" # catalog name -> $OUT_DIR/images/$IMAGE.ext4
FC_CI="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/x86_64"
BASE_URL="${BASE_URL:-$FC_CI/ubuntu-22.04.ext4}"
KERNEL_URL="${KERNEL_URL:-$FC_CI/vmlinux-5.10.223}"
GROW_MB="${GROW_MB:-768}" # headroom for the dev tools below
INIT_ASSET="${INIT_ASSET:-$REPO_ROOT/providers/compute/microvm/assets/hopbox-init}"
# Dev tools baked into the image so a box is usable out of the box. Override with
# PACKAGES="..."; PACKAGES="" bakes a bare rootfs.
PACKAGES="${PACKAGES:-git vim nano curl wget less ca-certificates openssh-client tmux htop python3}"

[ "$(id -u)" = 0 ] || { echo "must run as root (loop mount)"; exit 1; }
mkdir -p "$OUT_DIR" "$CACHE_DIR"

echo "==> binaries"
WORK="$(mktemp -d)"; trap 'umount -R "$WORK/mnt" 2>/dev/null || umount "$WORK/mnt" 2>/dev/null || true; rm -rf "$WORK"' EXIT
if [ -n "${AGENT_BIN:-}" ] && [ -n "${GUEST_BIN:-}" ]; then
  cp "$AGENT_BIN" "$WORK/hopbox-agent"; cp "$GUEST_BIN" "$WORK/box-guest"
elif command -v go >/dev/null; then
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$WORK/hopbox-agent" ./cmd/hopbox-agent )
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$WORK/box-guest"    ./cmd/box-guest )
else
  echo "no go and no \$AGENT_BIN/\$GUEST_BIN — supply prebuilt linux binaries"; exit 1
fi

echo "==> kernel + base rootfs (cached)"
[ -f "$CACHE_DIR/vmlinux" ]   || curl -fSL --retry 3 -o "$CACHE_DIR/vmlinux"   "$KERNEL_URL"
[ -f "$CACHE_DIR/$IMAGE-base.ext4" ] || curl -fSL --retry 3 -o "$CACHE_DIR/$IMAGE-base.ext4" "$BASE_URL"
cp "$CACHE_DIR/vmlinux" "$OUT_DIR/vmlinux"

echo "==> compose image '$IMAGE' (base + grow + inject)"
mkdir -p "$OUT_DIR/images"
IMG="$OUT_DIR/images/$IMAGE.ext4"
cp "$CACHE_DIR/$IMAGE-base.ext4" "$IMG"
truncate -s "+${GROW_MB}M" "$IMG"
e2fsck -fy "$IMG" >/dev/null 2>&1 || true
resize2fs "$IMG" >/dev/null 2>&1
mkdir -p "$WORK/mnt"; mount -o loop "$IMG" "$WORK/mnt"
# Inject the binaries first so the box is always functional, even if the optional
# tools layer below fails.
install -m0755 "$WORK/hopbox-agent" "$WORK/mnt/usr/local/bin/hopbox-agent"
install -m0755 "$WORK/box-guest"    "$WORK/mnt/usr/local/bin/box-guest"
install -m0755 "$INIT_ASSET"        "$WORK/mnt/sbin/hopbox-init"
# Best-effort dev tools via chroot apt. Some prebuilt base images (the Firecracker
# CI ubuntu rootfs) ship without a dpkg database and can't apt-install — skip with
# a note rather than fail. Use build-deboot.sh for a fully tooled image.
if [ -n "$PACKAGES" ] && [ -f "$WORK/mnt/var/lib/dpkg/status" ]; then
  echo "==> install dev tools (chroot apt): $PACKAGES"
  cp /etc/resolv.conf "$WORK/mnt/etc/resolv.conf" 2>/dev/null || true
  chmod 1777 "$WORK/mnt/tmp" 2>/dev/null || true
  mount -t proc proc "$WORK/mnt/proc"; mount -t sysfs sys "$WORK/mnt/sys"; mount -o bind /dev "$WORK/mnt/dev"
  chroot "$WORK/mnt" /bin/sh -c \
    "export DEBIAN_FRONTEND=noninteractive; apt-get update -qq && apt-get install -y --no-install-recommends $PACKAGES && apt-get clean && rm -rf /var/lib/apt/lists/*" \
    || echo "!! tool install failed — shipping a bare image (binaries are present)"
  umount "$WORK/mnt/dev" "$WORK/mnt/sys" "$WORK/mnt/proc"
elif [ -n "$PACKAGES" ]; then
  echo "==> skip dev tools: base has no dpkg database (apt-incapable image); use build-deboot.sh for tools"
fi
sync; umount "$WORK/mnt"

echo "==> done: kernel $OUT_DIR/vmlinux  image $IMG ($(du -h "$IMG" | cut -f1))"
