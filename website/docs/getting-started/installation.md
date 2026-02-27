---
sidebar_position: 1
---

# Installation

## Requirements

|                        | Minimum                                  |
| ---------------------- | ---------------------------------------- |
| **Developer machine**  | macOS or Linux                           |
| **VPS**                | Any Linux with systemd and a public IP   |
| **SSH access**         | Key-based authentication to the VPS      |

Windows is supported through WSL.

## Install the CLI

### Option 1: Install script

```bash
curl -fsSL https://get.hopbox.dev | sh
```

This downloads the latest `hop` binary for your platform and places it in `/usr/local/bin`.

### Option 2: Homebrew

```bash
brew install hopboxdev/tap/hop
```

### Option 3: Go install

```bash
go install github.com/hopboxdev/hopbox/cmd/hop@latest
```

Requires Go 1.22 or later.

### Option 4: Build from source

```bash
git clone https://github.com/hopboxdev/hopbox.git
cd hopbox
make build
```

Binaries are placed in `dist/`.

## Helper daemon

Hopbox uses a privileged helper daemon (`hop-helper`) to manage TUN devices and `/etc/hosts` entries. The helper is installed automatically during your first `hop setup` — it will prompt for `sudo` once.

**macOS:** Installed as a LaunchDaemon at `/Library/LaunchDaemons/dev.hopbox.helper.plist`. The helper binary lives at `/Library/PrivilegedHelperTools/dev.hopbox.helper`.

**Linux:** Installed as a systemd service at `/etc/systemd/system/hopbox-helper.service`. The helper binary lives at `/usr/local/bin/hop-helper`.

If you need to install the helper manually:

```bash
sudo hop-helper --install
```

## Verify installation

```bash
hop version
```

You should see the version number and build information.
