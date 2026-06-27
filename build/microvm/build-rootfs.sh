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
FC_CI="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/x86_64"
BASE_URL="${BASE_URL:-$FC_CI/ubuntu-22.04.ext4}"
KERNEL_URL="${KERNEL_URL:-$FC_CI/vmlinux-5.10.223}"
GROW_MB="${GROW_MB:-128}"
INIT_ASSET="${INIT_ASSET:-$REPO_ROOT/providers/compute/microvm/assets/hopbox-init}"

[ "$(id -u)" = 0 ] || { echo "must run as root (loop mount)"; exit 1; }
mkdir -p "$OUT_DIR" "$CACHE_DIR"

echo "==> binaries"
WORK="$(mktemp -d)"; trap 'umount "$WORK/mnt" 2>/dev/null || true; rm -rf "$WORK"' EXIT
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
[ -f "$CACHE_DIR/base.ext4" ] || curl -fSL --retry 3 -o "$CACHE_DIR/base.ext4" "$BASE_URL"
cp "$CACHE_DIR/vmlinux" "$OUT_DIR/vmlinux"

echo "==> compose agent.ext4 (base + grow + inject)"
IMG="$OUT_DIR/agent.ext4"
cp "$CACHE_DIR/base.ext4" "$IMG"
truncate -s "+${GROW_MB}M" "$IMG"
e2fsck -fy "$IMG" >/dev/null 2>&1 || true
resize2fs "$IMG" >/dev/null 2>&1
mkdir -p "$WORK/mnt"; mount -o loop "$IMG" "$WORK/mnt"
install -m0755 "$WORK/hopbox-agent" "$WORK/mnt/usr/local/bin/hopbox-agent"
install -m0755 "$WORK/box-guest"    "$WORK/mnt/usr/local/bin/box-guest"
install -m0755 "$INIT_ASSET"        "$WORK/mnt/sbin/hopbox-init"
sync; umount "$WORK/mnt"

echo "==> done: $OUT_DIR/vmlinux  $IMG ($(du -h "$IMG" | cut -f1))"
