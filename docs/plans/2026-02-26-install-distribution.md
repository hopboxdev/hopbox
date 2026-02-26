# Install Script & Homebrew Tap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** `curl -fsSL .../install.sh | sh` installs the `hop` binary, and `brew install hopboxdev/tap/hop` installs via Homebrew — both with SHA256 verification.

**Architecture:** A POSIX shell install script in the repo root that detects OS/arch, downloads the correct binary from GitHub Releases, verifies its checksum, and installs to `/usr/local/bin`. A goreleaser `brews` section auto-publishes a Homebrew formula to `hopboxdev/homebrew-tap` on each tagged release. The release workflow gets a PAT secret for cross-repo formula pushes.

**Tech Stack:** POSIX shell (install script), goreleaser brews config (YAML), GitHub Actions (workflow).

---

### Task 1: Create the install script

**Files:**
- Create: `install.sh`

**Step 1: Write the install script**

```sh
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
```

**Step 2: Make it executable and test locally**

Run: `chmod +x install.sh && shellcheck install.sh 2>/dev/null; echo "---" && sh -n install.sh && echo "syntax ok"`
Expected: no syntax errors (shellcheck warnings are advisory)

**Step 3: Commit**

```bash
git add install.sh
git commit -m "feat: add curl | sh install script"
```

---

### Task 2: Add goreleaser brews section

**Files:**
- Modify: `.goreleaser.yaml`

**Step 1: Add brews config**

Append to the end of `.goreleaser.yaml` (before any trailing newline):

```yaml
brews:
  - ids: [hop-archive]
    repository:
      owner: hopboxdev
      name: homebrew-tap
    directory: Formula
    homepage: "https://github.com/hopboxdev/hopbox"
    description: "Self-hostable workspace runtime for remote development"
    license: "Apache-2.0"
    install: |
      bin.install "hop"
    test: |
      system "#{bin}/hop", "version"
    extra_install: ""
    goamd64: v1
```

**Step 2: Validate goreleaser config**

Run: `goreleaser check 2>&1`
Expected: config is valid (or no errors related to brews section)

**Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "feat: add Homebrew tap config to goreleaser"
```

---

### Task 3: Update release workflow for cross-repo token

**Files:**
- Modify: `.github/workflows/release.yml`

The default `GITHUB_TOKEN` is scoped to the current repo. To push the formula to `hopboxdev/homebrew-tap`, goreleaser needs a token with `repo` scope on the tap repo. Use a PAT stored as `HOMEBREW_TAP_TOKEN`.

**Step 1: Update the workflow**

Replace the `GITHUB_TOKEN` env in the goreleaser step:

```yaml
        env:
          GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN || secrets.GITHUB_TOKEN }}
```

This uses the PAT if available, falls back to the default token (which works for everything except the tap push — the tap push will just warn and skip if no PAT is set).

**Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: use HOMEBREW_TAP_TOKEN for cross-repo formula push"
```

**Step 3: Note for user**

After merging, the user needs to:
1. Create the `hopboxdev/homebrew-tap` repo on GitHub with a `Formula/` directory
2. Create a GitHub PAT with `repo` scope (or fine-grained with contents:write on the tap repo)
3. Add it as `HOMEBREW_TAP_TOKEN` secret in the hopbox repo settings

---

### Task 4: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark installation item as complete**

Change: `- [ ] Installation script (\`curl | sh\`) + Homebrew tap + AUR`
To: `- [x] Installation script (\`curl | sh\`) + Homebrew tap`

(Drop AUR from the checked item since it's not included; it can be re-added as a separate item later if needed.)

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark install script and homebrew tap as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `install.sh` | Create — POSIX shell installer with checksum verification |
| `.goreleaser.yaml` | Modify — add `brews` section for Homebrew tap |
| `.github/workflows/release.yml` | Modify — add HOMEBREW_TAP_TOKEN fallback |
| `ROADMAP.md` | Modify — check off installation item |
