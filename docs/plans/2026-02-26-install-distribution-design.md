# Installation & Distribution Design

## Goal

Make hopbox easy to install for first-time users via two channels:
a `curl | sh` installer and a Homebrew tap.

## Install Script (`install.sh`)

POSIX shell script in the repo root, served via GitHub raw URL.

**Usage:**
```
curl -fsSL https://raw.githubusercontent.com/hopboxdev/hopbox/main/install.sh | sh
```

**Behavior:**
- Detects OS (`darwin`/`linux`) and architecture (`amd64`/`arm64`)
- Queries GitHub API for latest release (override with `HOP_VERSION=x.y.z`)
- Downloads the `hop` binary from GitHub Releases
- Verifies SHA256 against `checksums.txt`
- Installs to `/usr/local/bin/hop` (uses `sudo` if needed)
- Prints next-step instructions

**Out of scope:** Helper daemon installation — that happens on first `hop setup`.

**Error handling:** Fail fast on unsupported OS/arch, missing curl/wget,
checksum mismatch, or write permission issues.

## Homebrew Tap

**Repo:** `hopboxdev/homebrew-tap` — shared tap for all hopboxdev products.

**Formula:** `Formula/hop.rb` — downloads pre-built binary from GitHub Releases.

**Usage:**
```
brew tap hopboxdev/tap
brew install hop
```

**Auto-publish:** goreleaser's `brews` section pushes an updated formula to
`hopboxdev/homebrew-tap` on every tagged release. No manual formula maintenance.

**Details:**
- Formula installs only the `hop` client binary
- Sets `version.PackageManager = "homebrew"` via ldflags so `hop upgrade`
  tells users to use `brew upgrade` instead
- Tap repo must exist before first release (empty repo with `Formula/` dir)

## goreleaser Changes

Add a `brews` section to `.goreleaser.yaml` pointing at `hopboxdev/homebrew-tap`.
The existing `GITHUB_TOKEN` in the release workflow covers the tap repo since
it's in the same org.

## Files Touched

| File | Action |
|------|--------|
| `install.sh` | Create — POSIX shell install script |
| `.goreleaser.yaml` | Modify — add `brews` section |
| `.github/workflows/release.yml` | Review — ensure token permissions cover tap repo |
