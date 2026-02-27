---
sidebar_position: 4
---

# Agent API

The agent exposes an HTTP API at `http://<name>.hop:4200`. This API is only accessible over the WireGuard tunnel — it is never exposed to the public internet.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check and version info |
| `/rpc` | POST | JSON-RPC method dispatcher |

## GET /health

Returns agent status.

**Response:**

```json
{
  "status": "ok",
  "tunnel": true,
  "local_ip": "10.10.0.2",
  "version": "0.4.0"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Always `"ok"` |
| `tunnel` | bool | Whether WireGuard interface is up |
| `local_ip` | string | Assigned WireGuard IP |
| `version` | string | Agent binary version |

## POST /rpc

All RPC calls use the same envelope format.

**Request:**

```json
{
  "method": "services.list",
  "params": {}
}
```

**Success response:**

```json
{
  "result": { ... }
}
```

**Error response:**

```json
{
  "error": "error message"
}
```

Request body is limited to 1 MiB.

---

## RPC Methods

### services.list

List all managed services with their status.

**Request:**

```json
{"method": "services.list"}
```

**Response:**

```json
{
  "result": [
    {"name": "postgres", "running": true, "type": "docker", "error": ""},
    {"name": "api", "running": true, "type": "native", "error": ""}
  ]
}
```

### services.restart

Restart a single service.

**Request:**

```json
{"method": "services.restart", "params": {"name": "postgres"}}
```

**Response:**

```json
{"result": {"status": "restarted"}}
```

### services.stop

Stop a single service.

**Request:**

```json
{"method": "services.stop", "params": {"name": "api"}}
```

**Response:**

```json
{"result": {"status": "stopped"}}
```

### ports.list

List all TCP ports in LISTEN state on the server.

**Request:**

```json
{"method": "ports.list"}
```

**Response:**

```json
{
  "result": [
    {"port": 8080, "program": "node"},
    {"port": 5432, "program": "postgres"}
  ]
}
```

Port discovery reads `/proc/net/tcp` and resolves program names from `/proc/<pid>/cmdline`.

### run.script

Execute a named script from the manifest `scripts:` section.

**Request:**

```json
{"method": "run.script", "params": {"name": "build"}}
```

**Response:**

```json
{"result": {"output": "Building...\nSuccess!\n"}}
```

### logs.stream

Stream service logs as plain text.

**Request (single service):**

```json
{"method": "logs.stream", "params": {"name": "postgres"}}
```

**Request (all services):**

```json
{"method": "logs.stream"}
```

This is the only method that does **not** return a JSON envelope. It streams `text/plain` output directly (equivalent to `docker logs --follow`). The stream ends when the client disconnects.

### packages.install

Install system packages.

**Request:**

```json
{
  "method": "packages.install",
  "params": {
    "packages": [
      {"name": "curl", "backend": "apt"},
      {"name": "nodejs", "backend": "nix"}
    ]
  }
}
```

**Response:**

```json
{"result": {"installed": ["curl", "nodejs"]}}
```

### snap.create

Create a workspace snapshot.

**Request:**

```json
{"method": "snap.create"}
```

**Response:**

```json
{
  "result": {
    "snapshot_id": "a1b2c3d4",
    "files_new": 150,
    "added_size": 52428800
  }
}
```

Backs up all service data directories. Each snapshot is tagged with the manifest SHA256.

### snap.list

List available snapshots.

**Request:**

```json
{"method": "snap.list"}
```

**Response:**

```json
{
  "result": [
    {
      "id": "a1b2c3d4567890ab",
      "short_id": "a1b2c3d4",
      "time": "2026-02-27T15:30:00Z",
      "paths": ["/opt/hopbox/data"],
      "hostname": "mybox",
      "tags": ["hopbox-manifest:abc123de"]
    }
  ]
}
```

### snap.restore

Restore a snapshot by ID.

**Request:**

```json
{"method": "snap.restore", "params": {"id": "a1b2c3d4"}}
```

**Response:**

```json
{"result": {"status": "restored", "id": "a1b2c3d4"}}
```

Optionally pass `"restore_path": "/"` to control the restore target.

### workspace.sync

Reload the manifest and restart services.

**Request:**

```json
{
  "method": "workspace.sync",
  "params": {
    "yaml": "name: myproject\nservices:\n  web:\n    type: docker\n    image: nginx:latest\n    ports:\n      - \"8080:80\""
  }
}
```

**Response:**

```json
{"result": {"status": "synced", "name": "myproject"}}
```

Stops old services and starts new ones from the provided manifest YAML.

---

## Error codes

| HTTP Status | Meaning |
|-------------|---------|
| 400 | Invalid parameters or missing required fields |
| 404 | Unknown RPC method |
| 405 | Non-POST request to `/rpc` |
| 500 | Execution failure |
| 503 | Feature not configured (e.g., no backup target) |
