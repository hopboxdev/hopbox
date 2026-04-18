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

# Build in-container hop CLI
echo "    building hop CLI..."
GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${STAGE_DIR}/templates/hop" ./cmd/hop-box

# Copy templates (directories and individual files)
cp -r templates/base-devcontainer "${STAGE_DIR}/templates/"
cp -r templates/builder "${STAGE_DIR}/templates/"
cp templates/ghostty.terminfo "${STAGE_DIR}/templates/ghostty.terminfo"

# Copy top-level config example
cp config.example.toml "${STAGE_DIR}/config.example.toml"

# Copy systemd unit
cp deploy/hopboxd.service "${STAGE_DIR}/deploy/hopboxd.service"

# Copy monitoring stack (entire directory — compose, prometheus, grafana provisioning and dashboards)
mkdir -p "${STAGE_DIR}/deploy/monitoring"
cp -R deploy/monitoring/. "${STAGE_DIR}/deploy/monitoring/"

# Strip README from monitoring dir (not needed in release)
rm -f "${STAGE_DIR}/deploy/monitoring/README.md"

# Copy Caddy example config
mkdir -p "${STAGE_DIR}/deploy/caddy"
cp deploy/caddy/Caddyfile.example "${STAGE_DIR}/deploy/caddy/Caddyfile.example"

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

# Clean up staging directory
rm -rf "${STAGE_DIR}"
