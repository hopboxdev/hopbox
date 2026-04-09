# Hopbox Phase 2 Design — Interactive Tool Selection & Per-User Images

## Overview

Phase 2 adds an interactive TUI wizard for tool selection, per-user Docker images, and automatic container rebuilds. Users configure their dev environment (multiplexer, editor, shell, runtimes, CLI tools) through a charmbracelet/huh form over SSH. Each user gets a custom Docker image layered on the shared base.

**Goal:** First-time users see a tool selection wizard, their choices build a custom image, and they land in a container tailored to their preferences. Subsequent connections use the saved profile. New boxnames show the wizard pre-filled with the user's defaults.

## Profile Format

**`profile.toml`:**
```toml
[multiplexer]
tool = "zellij"  # "zellij" | "tmux"

[editor]
tool = "neovim"  # "neovim" | "vim" | "none"

[shell]
tool = "bash"  # "bash" | "zsh" | "fish"

[runtimes]
node = "lts"      # "lts" | "latest" | "none"
python = "3.12"   # "3.12" | "3.13" | "none"
go = "none"       # "latest" | "none"
rust = "none"     # "latest" | "none"

[tools]
extras = ["fzf", "ripgrep", "fd", "bat", "lazygit"]
```

Available CLI tools: fzf, ripgrep, fd, bat, lazygit, direnv.

## Storage Layout

```
data/users/<fingerprint>/
├── user.toml              # username, key type (existing from Phase 1)
├── profile.toml           # user's default profile
└── boxes/
    ├── default/
    │   ├── profile.toml   # box-specific override (absent = use default)
    │   └── home/
    └── project1/
        ├── profile.toml
        └── home/
```

**Profile resolution:** When connecting to a box, if `boxes/<boxname>/profile.toml` exists, use it. Otherwise fall back to the user-level `profile.toml`.

## Wizard Flow

**When the wizard runs:**
- First connection (new user): registration TUI (username) → wizard with hardcoded defaults → save as user-level `profile.toml`
- First connection to a new boxname: wizard pre-filled with user's default profile → save as `boxes/<boxname>/profile.toml`
- Existing box with profile: skip wizard, go straight to container

**Wizard structure:**
- Single `huh.NewForm()` with grouped fields (headers as visual separators)
- Groups: Terminal Multiplexer, Editor, Shell, Runtimes, CLI Tools
- Single-choice selects for multiplexer, editor, shell
- Version selects for runtimes (including "none" to skip)
- Multi-select checkboxes for CLI tools
- Confirm/Cancel at the end

**Hardcoded defaults (first-time users):**
- Multiplexer: zellij
- Editor: neovim
- Shell: bash
- Node: lts, Python: 3.12, Go: none, Rust: none
- Tools: fzf, ripgrep, fd, bat, lazygit

**On cancel:** Use defaults without saving a profile — creates container with default tools. Profile will be saved if the user completes the wizard on a subsequent connection.

## Per-User Image Building

**Image layering:**
```
hopbox-base:<hash>                ← ubuntu 24.04 + apt basics + mise binary (shared)
    └── hopbox-<username>:<hash>  ← user's tools layered on top
```

**Base image (`hopbox-base`) — slimmed down from Phase 1:**
- Ubuntu 24.04
- sudo, curl, wget, git, build-essential, openssh-client, unzip, xz-utils, ca-certificates
- mise binary installed to `/usr/local/bin/mise`
- `dev` user (UID 1000) with sudo
- mise data/config at `/opt/mise` (outside bind-mounted home)
- mise activation in `/etc/bash.bashrc`

The base image no longer includes runtimes, editors, multiplexers, or CLI tools. Those move to per-user images.

**Per-user image (`hopbox-<username>:<profileHash>`):**

Generated Dockerfile from profile:
```dockerfile
FROM hopbox-base:<baseHash>

USER root

# Shell (if not bash)
RUN apt-get update && apt-get install -y zsh && rm -rf /var/lib/apt/lists/*

# Multiplexer
RUN curl -fsSL https://github.com/zellij-org/zellij/releases/latest/download/zellij-aarch64-unknown-linux-musl.tar.gz | tar -xz -C /usr/local/bin/

# Editor
RUN curl -fsSL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz | tar -xz -C /opt/ \
    && ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim

# CLI Tools (only selected ones)
RUN apt-get update && apt-get install -y ripgrep fd-find bat && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
    && ln -sf /usr/bin/batcat /usr/local/bin/bat
# ... additional tool installs

USER dev
WORKDIR /home/dev

# Runtimes (only selected ones)
RUN mise install node@lts && mise use --global node@lts
RUN mise install python@3.12 && mise use --global python@3.12

CMD ["sleep", "infinity"]
```

**Image hash:** The profile is serialized and hashed → `hopbox-<username>:<first12chars>`. On connect, check if the tag exists. If not, generate Dockerfile and build. If two boxes share the same profile, they share the same image (same hash). Different profiles produce different images automatically.

**Architecture detection:** Install scripts must detect `aarch64` vs `x86_64` and download the correct binary. The builder generates the appropriate download URLs based on `runtime.GOARCH`.

## Container Lifecycle Changes

**Phase 2 connect flow:**
```
connect → resolve profile (box-level or user-level)
  → compute profile hash → derive image tag
  → if image doesn't exist: generate Dockerfile, build image
  → find container by name
    → if exists but label mismatches profile hash: stop, remove, recreate
    → if exists and matches: start if stopped
    → if not found: create with profile hash label
  → exec into container (multiplexer + shell from profile)
```

**Container labels:**
- `hopbox.profile-hash=<hash>` — tracks which profile the container was built from
- On connect, compare container's label to current profile hash
- Mismatch → container is outdated → stop, remove, recreate from new image

**Adaptive exec command:**
- zellij: `bash -c 'export TERM=...; export SHELL=...; exec zellij attach --create default'`
- tmux: `bash -c 'export TERM=...; exec tmux new-session -As default'`
- Shell env set from profile (`/bin/bash`, `/usr/bin/zsh`, `/usr/bin/fish`)

**Home dir preserved:** On rebuild, only the container is replaced. The bind-mounted home directory is untouched.

## Session Handler Changes

The session handler in `server.go` chains:

```
1. Auth (existing)
2. Registration — if new user (existing)
3. Profile resolution — load box-level or user-level profile
4. Wizard — if no profile found, run tool selection TUI
5. Image check — ensure per-user image exists for profile hash
6. Container lifecycle — ensure container running with correct image
7. Exec — attach to multiplexer (existing, but adaptive)
```

Steps 3-5 are new. Step 7 is modified to use profile values.

## File Structure Changes

**New files:**
```
internal/
├── wizard/
│   └── wizard.go              # huh form for tool selection, returns Profile
├── users/
│   ├── profile.go             # Profile struct, TOML read/write, hash, resolve
│   └── profile_test.go
├── containers/
│   ├── builder.go             # generate per-user Dockerfile from Profile, build
│   └── builder_test.go
```

**Modified files:**
```
internal/
├── gateway/
│   └── server.go              # session handler chains wizard + profile
├── containers/
│   ├── manager.go             # label tracking, image mismatch, adaptive exec
│   └── image.go               # base image slimmed (no runtimes/tools)
├── templates/
│   └── Dockerfile.base        # slimmed: ubuntu + apt basics + mise only
```

**Removed files:**
```
templates/stacks/tools.sh      # replaced by builder.go
templates/stacks/runtimes.sh   # replaced by builder.go
```

## What Phase 2 Does NOT Include

- Dotfiles repo integration (deferred — no clean implementation path yet)
- `hopbox rebuild` command inside the devbox (profile changes trigger auto-rebuild on next connect)
- `hopbox config` command to re-run wizard from inside the box
- `hopbox rebuild` / `hopbox config` commands inside the devbox (future phases)
