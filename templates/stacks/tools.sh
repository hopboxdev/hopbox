#!/bin/bash
set -euo pipefail

# fzf
curl -fsSL https://github.com/junegunn/fzf/releases/latest/download/fzf-$(curl -s https://api.github.com/repos/junegunn/fzf/releases/latest | grep -o '"tag_name": "v[^"]*' | cut -d'v' -f2)-linux_amd64.tar.gz | tar -xz -C /usr/local/bin/

# ripgrep
apt-get update && apt-get install -y ripgrep

# fd
apt-get install -y fd-find
ln -sf /usr/bin/fdfind /usr/local/bin/fd

# bat
apt-get install -y bat
ln -sf /usr/bin/batcat /usr/local/bin/bat

# lazygit
LAZYGIT_VERSION=$(curl -s https://api.github.com/repos/jesseduffield/lazygit/releases/latest | grep -o '"tag_name": "v[^"]*' | cut -d'v' -f2)
curl -fsSL "https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" | tar -xz -C /usr/local/bin/ lazygit

# direnv
curl -sfL https://direnv.net/install.sh | bash

# zellij
curl -fsSL https://github.com/zellij-org/zellij/releases/latest/download/zellij-x86_64-unknown-linux-musl.tar.gz | tar -xz -C /usr/local/bin/

# neovim
curl -fsSL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz | tar -xz -C /opt/
ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim

rm -rf /var/lib/apt/lists/*
