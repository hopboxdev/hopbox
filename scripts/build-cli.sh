#!/bin/bash
set -euo pipefail

# Cross-compile the hopbox CLI for linux
# Uses the host's GOARCH to match the Docker container architecture

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 \
    go build -o "$PROJECT_ROOT/templates/hop" "$PROJECT_ROOT/cmd/hop-box/"

echo "Built templates/hop for linux/$(go env GOARCH)"
