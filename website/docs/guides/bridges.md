---
sidebar_position: 4
---

# Bridges

Bridges connect inherently local resources (clipboard, browser, desktop notifications) between your developer machine and the remote workspace. They are distinct from port forwarding — any TCP/UDP port is already reachable over the WireGuard tunnel without bridge code.

## Bridge types

### Clipboard

Bidirectional clipboard sync between client and server.

```yaml
bridges:
  - type: clipboard
```

The server sends clipboard content over a TCP connection (port 2224). On the client side, `pbcopy` (macOS) or `xclip`/`xsel` (Linux) receives the data. Clipboard reads use the corresponding paste commands.

Maximum payload: 1 MB per clipboard update.

### Chrome DevTools Protocol (CDP)

Forwards Chrome DevTools Protocol connections so remote tools can debug a local Chrome instance.

```yaml
bridges:
  - type: cdp
```

The bridge listens on port 9222 (the standard CDP port) and proxies connections to Chrome running on your local machine. This enables remote dev tools and testing frameworks to connect to your local browser.

### xdg-open

Opens URLs from the server in your local browser.

```yaml
bridges:
  - type: xdg-open
```

When code on the server calls `xdg-open https://example.com`, the URL is sent over TCP (port 2225) to the client, which opens it with `open` (macOS) or `xdg-open` (Linux).

### Notifications

Displays desktop notifications from the server on your local machine.

```yaml
bridges:
  - type: notifications
```

The server sends JSON payloads with `title` and `body` fields over TCP (port 2226). The client displays them using `osascript` (macOS) or `notify-send` (Linux).

## Configuration

Declare bridges in the `bridges:` section of `hopbox.yaml`:

```yaml
bridges:
  - type: clipboard
  - type: cdp
  - type: xdg-open
  - type: notifications
```

Bridges start automatically when you run `hop up` and stop when you run `hop down`.

## Managing bridges

```bash
# List active bridges
hop bridge ls

# Restart all bridges
hop bridge restart
```

## Port forwarding

Port forwarding is separate from bridges and runs automatically. The port forwarder polls the server every 3 seconds to discover listening ports and creates local proxies:

```
Forwarding localhost:8080
Forwarding localhost:3000
```

Excluded ports: 22 (SSH), 4200 (agent API), 51820 (WireGuard). Ports already in use locally are silently skipped.

When a server process stops listening, the local proxy is removed:

```
Stopped forwarding localhost:8080
```
