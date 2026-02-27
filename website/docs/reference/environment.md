---
sidebar_position: 3
---

# Environment Variables

Hopbox loads environment variables from multiple sources and merges them in a defined order.

## Sources and precedence

Environment variables are merged in this order (last wins):

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (lowest) | `.env` | Shared defaults, committed to version control |
| 2 | `.env.local` | Personal overrides, gitignored by convention |
| 3 | `env:` in `hopbox.yaml` | Manifest-level environment |
| 4 (highest) | Service-level `env:` | Per-service overrides |

## .env files

Place `.env` and `.env.local` files next to your `hopbox.yaml`:

```
myproject/
  hopbox.yaml
  .env          # shared defaults
  .env.local    # personal overrides (gitignored)
```

### .env

Shared environment variables committed to version control:

```bash
DATABASE_URL=postgres://localhost:5432/dev
NODE_ENV=development
LOG_LEVEL=info
```

### .env.local

Personal overrides that should not be committed:

```bash
DATABASE_URL=postgres://localhost:5432/mylocal
API_SECRET=my-secret-key
```

Add `.env.local` to your `.gitignore`.

## Manifest env

The `env:` section in `hopbox.yaml` sets workspace-level variables:

```yaml
env:
  NODE_ENV: development
  LOG_LEVEL: debug
```

## Service-level env

Per-service variables override everything else:

```yaml
services:
  postgres:
    type: docker
    image: postgres:16
    env:
      POSTGRES_PASSWORD: dev
      POSTGRES_DB: myapp
```

## When changes take effect

Services are always recreated on `hop up`. When you change environment variables in any source and run `hop up` again, the agent stops old services and starts fresh ones with the updated environment. No manual restart is needed.

## Example

Given these files:

```bash title=".env"
PORT=3000
DATABASE_URL=postgres://localhost/dev
```

```bash title=".env.local"
DATABASE_URL=postgres://localhost/mylocal
```

```yaml title="hopbox.yaml"
env:
  PORT: "8080"

services:
  api:
    type: native
    command: node server.js
    env:
      LOG_LEVEL: debug
```

The `api` service sees:

| Variable | Value | Source |
|----------|-------|-------|
| `PORT` | `8080` | manifest `env:` overrides `.env` |
| `DATABASE_URL` | `postgres://localhost/mylocal` | `.env.local` overrides `.env` |
| `LOG_LEVEL` | `debug` | service-level `env:` |
