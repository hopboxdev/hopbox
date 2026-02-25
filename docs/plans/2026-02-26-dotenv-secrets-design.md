# Dotenv Secrets Design

## Problem

Secrets (API keys, tokens, database passwords) need to reach services on the
agent, but shouldn't be committed plaintext in `hopbox.yaml`. Today there's no
.env file support, and `Workspace.Env` is parsed but never applied to services.

## Decision

Formalize the `.env` convention. `hop up` reads `.env` and `.env.local` from the
workspace directory, merges them into `Workspace.Env`, and syncs via the existing
`workspace.sync` RPC. No encryption layer, no CLI commands, no new RPCs.

## .env File Format

Standard dotenv: `KEY=VALUE`, `# comments`, blank lines, quoted values
(`"double"` and `'single'`). No variable interpolation.

## Load & Merge Order

Files loaded (last wins within files):
1. `.env` — shared defaults, safe to commit
2. `.env.local` — local overrides, gitignored, actual secrets

Merge precedence (last wins):
1. `.env` values
2. `.env.local` values
3. `env:` in `hopbox.yaml` (workspace-level)
4. Per-service `env:` in `hopbox.yaml`

Manifest values override .env files — the manifest is the source of truth.

## Agent-Side Merge

`BuildServiceManager` merges `Workspace.Env` into every service's env map.
Service-level env overrides workspace-level. The agent has no knowledge of .env
files — it just receives a manifest with a populated `env:` field.

## Implementation Scope

1. **New `internal/dotenv` package** — parser for .env files (no external deps)
2. **Fix `BuildServiceManager`** — apply `Workspace.Env` to all services
3. **Client-side loading in `cmd/hop/up.go`** — read .env files, merge into
   manifest before sync
4. **Remove `Secret` struct** — the `secrets:` manifest field is replaced by .env
5. **Logging** — print "Loaded .env (N vars)" during hop up

## What Does NOT Change

- `workspace.sync` RPC payload and protocol
- Agent API
- Service backends (Docker/Native) — they already handle `Service.Env`
- Per-service `env:` field in hopbox.yaml

## Security Notes

- .env files are read client-side and values travel over WireGuard (encrypted)
- Agent persists the synced manifest to `/etc/hopbox/hopbox.yaml` with resolved
  env values. Acceptable for a user-owned VPS. Can add redaction later if needed.
- `.env.local` should be gitignored
