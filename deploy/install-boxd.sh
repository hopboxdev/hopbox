#!/bin/sh
# boxd installer — the standalone compute-box product (`ssh box@host`).
#
#   curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/deploy/install-boxd.sh | sudo sh
#
# Installs boxd + the in-box agent + box-guest, writes a systemd service, and
# starts it on the docker backend (zero extra setup). Idempotent: re-run to
# upgrade or reconfigure. Settings live in /etc/hopbox/boxd.env.
#
# Backends:
#   docker  (default) — boxes are containers; only Docker is required.
#   microvm           — boxes are Firecracker microVMs (auto-suspend/wake). Needs
#                       /dev/kvm + a golden rootfs (build/microvm/build-rootfs.sh).
#                       Set HOPBOX_COMPUTE=microvm and the fc-* paths below.
#
# Overrides (first run): HOPBOX_VERSION, HOPBOX_COMPUTE.
set -eu

REPO="hopboxdev/hopbox"
PREFIX="/usr/local/bin"
LIBDIR="/var/lib/hopbox"
ETCDIR="/etc/hopbox"
ENVFILE="$ETCDIR/boxd.env"
VERSION="${HOPBOX_VERSION:-latest}"
COMPUTE="${HOPBOX_COMPUTE:-docker}"

log() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root (e.g. via sudo)"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"
case "$(uname -s)" in Linux) ;; *) die "Linux only" ;; esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported arch $(uname -m)" ;;
esac
log "platform: linux/$ARCH  backend: $COMPUTE"

[ "$COMPUTE" = docker ] && ! command -v docker >/dev/null 2>&1 && die "the docker backend needs Docker installed"
[ "$COMPUTE" = microvm ] && [ ! -e /dev/kvm ] && die "the microvm backend needs /dev/kvm (KVM / nested virt)"

# --- download the box bundle ---
base="https://github.com/$REPO/releases"
if [ "$VERSION" = latest ]; then dl="$base/latest/download"; else dl="$base/download/$VERSION"; fi
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
asset="hopbox-box_linux_$ARCH.tar.gz"
log "downloading $asset"
curl -fsSL -o "$tmp/$asset" "$dl/$asset" || die "download failed: $dl/$asset"
tar -xzf "$tmp/$asset" -C "$tmp" || die "could not extract $asset"

# --- install binaries ---
install -m755 "$tmp/boxd" "$tmp/box-guest" "$PREFIX/"
mkdir -p "$LIBDIR"
install -m755 "$tmp/hopbox-agent" "$LIBDIR/hopbox-agent-linux-$ARCH"
install -m755 "$tmp/box-guest"    "$LIBDIR/box-guest-linux-$ARCH"
log "installed boxd + box-guest to $PREFIX; agent + box-guest to $LIBDIR"

# --- config (created once) ---
mkdir -p "$ETCDIR"
if [ ! -f "$ENVFILE" ]; then
  cat > "$ENVFILE" <<EOF
# boxd configuration. Edit, then: systemctl restart boxd
HOPBOX_COMPUTE=$COMPUTE
HOPBOX_SSH_ADDR=:2222
HOPBOX_AGENT_LISTEN=:7777
HOPBOX_META_ADDR=:8090
HOPBOX_DB=$LIBDIR/boxd.db
HOPBOX_HOST_KEY=$LIBDIR/boxd-ssh-host-key
# docker backend: binaries side-loaded into each box
HOPBOX_AGENT_BIN=$LIBDIR/hopbox-agent-linux-$ARCH
HOPBOX_GUEST_BIN=$LIBDIR/box-guest-linux-$ARCH
# microvm backend: kernel + image catalog (build with build/microvm/build-rootfs.sh)
HOPBOX_FC_KERNEL=/opt/hopbox-microvm/vmlinux
HOPBOX_FC_IMAGES_DIR=/opt/hopbox-microvm/images
HOPBOX_DEFAULT_IMAGE=ubuntu-22.04
HOPBOX_FC_RUNDIR=$LIBDIR/microvm
# persistent boxes that auto-suspend when idle (vs ephemeral reap):
HOPBOX_AUTO_SUSPEND=
HOPBOX_IDLE_TIMEOUT=5m
EOF
  log "wrote default config $ENVFILE"
else
  log "keeping existing config $ENVFILE"
fi
# shellcheck disable=SC1090
. "$ENVFILE"

# --- systemd unit ---
cat > /etc/systemd/system/boxd.service <<EOF
[Unit]
Description=hopbox compute-box daemon (boxd)
After=network.target docker.service

[Service]
EnvironmentFile=$ENVFILE
ExecStart=$PREFIX/boxd --compute \${HOPBOX_COMPUTE} \\
  --ssh-addr \${HOPBOX_SSH_ADDR} --agent-listen \${HOPBOX_AGENT_LISTEN} --meta-addr \${HOPBOX_META_ADDR} \\
  --db \${HOPBOX_DB} --host-key \${HOPBOX_HOST_KEY} \\
  --agent-bin \${HOPBOX_AGENT_BIN} --guest-bin \${HOPBOX_GUEST_BIN} \\
  --fc-kernel \${HOPBOX_FC_KERNEL} --fc-images-dir \${HOPBOX_FC_IMAGES_DIR} --fc-rundir \${HOPBOX_FC_RUNDIR} \\
  --default-image \${HOPBOX_DEFAULT_IMAGE} \\
  \${HOPBOX_AUTO_SUSPEND:+--auto-suspend} --idle-timeout \${HOPBOX_IDLE_TIMEOUT}
Restart=on-failure
RestartSec=2
User=root

[Install]
WantedBy=multi-user.target
EOF
log "installed systemd unit boxd.service"

systemctl daemon-reload
systemctl enable --now boxd.service
sleep 1
log "boxd: $(systemctl is-active boxd)"

cat <<EOF

boxd is installed and running ($COMPUTE backend).

  Config:  $ENVFILE  (edit + 'systemctl restart boxd' to apply)
  Connect: ssh box@<this-host> -p 2222     # spawns a box and drops you in

EOF
if [ "$COMPUTE" = microvm ]; then
cat <<EOF
microVM backend selected — build the image catalog first (once), then restart:
  # on a machine with Go (or pass prebuilt AGENT_BIN/GUEST_BIN):
  sudo IMAGE=ubuntu-22.04 OUT_DIR=/opt/hopbox-microvm build/microvm/build-rootfs.sh
  systemctl restart boxd
  # add more images:  sudo IMAGE=debian-default BASE_URL=<debian.ext4> ... build-rootfs.sh
EOF
fi
echo "Boxes are anonymous (key = identity). Keep the SSH front door where you want it."
