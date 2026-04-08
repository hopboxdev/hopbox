#!/bin/bash
set -euo pipefail

# Install mise (runtime version manager)
curl https://mise.run | sh
mv /root/.local/bin/mise /usr/local/bin/mise

# Create mise config directory for dev user
mkdir -p /home/dev/.config/mise
chown -R dev:dev /home/dev/.config
