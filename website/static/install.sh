#!/bin/sh
# Hopbox installer — downloads the hop CLI binary from GitHub Releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/install.sh | sh
#
# Environment variables:
#   HOP_VERSION   — specific version to install (e.g. "0.5.0"). Default: latest.
#   HOP_INSTALL   — install directory. Default: /usr/local/bin.

set -e

REPO="hopboxdev/hopbox"
INSTALL_DIR="${HOP_INSTALL:-/usr/local/bin}"

main() {
    need_cmd curl
    need_cmd uname

    os="$(detect_os)"
    arch="$(detect_arch)"

    if [ -z "$HOP_VERSION" ]; then
        printf "Fetching latest release... "
        HOP_VERSION="$(latest_version)"
        printf "v%s\n" "$HOP_VERSION"
    fi

    bin_name="hop_${HOP_VERSION}_${os}_${arch}"
    bin_url="https://github.com/${REPO}/releases/download/v${HOP_VERSION}/${bin_name}"
    cs_url="https://github.com/${REPO}/releases/download/v${HOP_VERSION}/checksums.txt"

    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' EXIT

    printf "Downloading hop v%s (%s/%s)... " "$HOP_VERSION" "$os" "$arch"
    curl -fsSL -o "${tmpdir}/hop" "$bin_url"
    printf "done\n"

    printf "Verifying checksum... "
    curl -fsSL -o "${tmpdir}/checksums.txt" "$cs_url"
    verify_checksum "${tmpdir}/hop" "${tmpdir}/checksums.txt" "$bin_name"
    printf "ok\n"

    chmod +x "${tmpdir}/hop"

    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmpdir}/hop" "${INSTALL_DIR}/hop"
    else
        printf "Installing to %s (requires sudo)...\n" "$INSTALL_DIR"
        sudo mv "${tmpdir}/hop" "${INSTALL_DIR}/hop"
    fi

    printf "\nhop v%s installed to %s/hop\n" "$HOP_VERSION" "$INSTALL_DIR"
    printf "Run 'hop setup <name> -a <ip>' to get started.\n"
}

detect_os() {
    case "$(uname -s)" in
        Darwin*) echo "darwin" ;;
        Linux*)  echo "linux" ;;
        *)       err "unsupported OS: $(uname -s). Hopbox supports macOS and Linux." ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             err "unsupported architecture: $(uname -m). Hopbox supports amd64 and arm64." ;;
    esac
}

latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed 's/.*"v\([^"]*\)".*/\1/'
}

verify_checksum() {
    file="$1"
    checksums_file="$2"
    expected_name="$3"

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')"
    else
        printf "warning: no sha256sum or shasum found, skipping checksum verification\n"
        return 0
    fi

    expected="$(grep "$expected_name" "$checksums_file" | awk '{print $1}')"
    if [ -z "$expected" ]; then
        err "checksum entry not found for $expected_name"
    fi

    if [ "$actual" != "$expected" ]; then
        err "checksum mismatch: got $actual, expected $expected"
    fi
}

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        err "required command not found: $1"
    fi
}

err() {
    printf "error: %s\n" "$1" >&2
    exit 1
}

main "$@"
