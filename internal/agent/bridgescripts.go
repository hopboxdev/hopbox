package agent

import (
	"os"
	"path/filepath"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

const bridgeScriptDir = "/opt/hopbox/bin"

// xdgOpenScript is installed as /opt/hopbox/bin/xdg-open on the server.
// It sends the URL to the client's bridge listener over the WireGuard tunnel.
// Uses bash /dev/tcp (no external dependencies), with nc as fallback.
var xdgOpenScript = `#!/bin/bash
# hopbox xdg-open bridge — forwards URLs to the client's local browser.
# Usage: xdg-open <url>
URL="$1"
[ -z "$URL" ] && exit 0
HOST=` + tunnel.ClientIP + `
PORT=2225
# Try bash built-in /dev/tcp first (no deps), fall back to nc.
if (echo "" > /dev/tcp/$HOST/$PORT) 2>/dev/null; then
  exec 3<>/dev/tcp/$HOST/$PORT
  printf '%s\n' "$URL" >&3
  exec 3>&-
elif command -v nc >/dev/null 2>&1; then
  printf '%s\n' "$URL" | nc -w 2 $HOST $PORT 2>/dev/null || true
fi
`

// notifySendScript is installed as /opt/hopbox/bin/notify-send on the server.
// It constructs a JSON payload and sends it to the client's notification bridge.
var notifySendScript = `#!/bin/bash
# hopbox notify-send bridge — forwards notifications to the client's desktop.
# Usage: notify-send <title> [body]
TITLE="${1:-Notification}"
BODY="${2:-}"
HOST=` + tunnel.ClientIP + `
PORT=2226
# Escape double quotes and backslashes for JSON.
TITLE=$(printf '%s' "$TITLE" | sed 's/\\/\\\\/g; s/"/\\"/g')
BODY=$(printf '%s' "$BODY" | sed 's/\\/\\\\/g; s/"/\\"/g')
JSON=$(printf '{"title":"%s","body":"%s"}' "$TITLE" "$BODY")
# Try bash built-in /dev/tcp first (no deps), fall back to nc.
if (echo "" > /dev/tcp/$HOST/$PORT) 2>/dev/null; then
  exec 3<>/dev/tcp/$HOST/$PORT
  printf '%s' "$JSON" >&3
  exec 3>&-
elif command -v nc >/dev/null 2>&1; then
  printf '%s' "$JSON" | nc -w 2 $HOST $PORT 2>/dev/null || true
fi
`

// InstallBridgeScripts writes server-side shim scripts for xdg-open and
// notify-send to /opt/hopbox/bin/ (which is in the agent's PATH). Only
// scripts for bridges declared in the workspace manifest are installed.
func InstallBridgeScripts(ws *manifest.Workspace) error {
	return installBridgeScriptsTo(ws, bridgeScriptDir)
}

// installBridgeScriptsTo is the testable implementation of InstallBridgeScripts.
func installBridgeScriptsTo(ws *manifest.Workspace, dir string) error {
	if ws == nil {
		return nil
	}

	scripts := map[string]string{}
	for _, b := range ws.Bridges {
		switch b.Type {
		case "xdg-open":
			scripts["xdg-open"] = xdgOpenScript
		case "notifications":
			scripts["notify-send"] = notifySendScript
		}
	}

	if len(scripts) == 0 {
		return nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	for name, content := range scripts {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return err
		}
		if err := os.Chmod(path, 0755); err != nil {
			return err
		}
	}

	return nil
}
