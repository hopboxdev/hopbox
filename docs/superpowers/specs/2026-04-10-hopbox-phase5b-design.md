# Hopbox Phase 5B Design — Shared Devboxes via Key Linking

## Overview

Phase 5B lets users access the same boxes from multiple machines by linking SSH keys. A user generates a one-time link code inside their container, then connects from another machine and enters the code to link the new key to their existing account.

**Goal:** `hopbox link` generates a code inside a container. From another machine, connecting with an unknown key shows a choice: "Create new account" or "Link to existing account". Entering the code links the new key and connects to the user's boxes.

## Link Code Generation

Inside a container, user runs `hopbox link`:
- CLI sends `{"command": "link"}` via control socket
- hopboxd generates an 8-character code formatted as `XXXX-XXXX` (alphanumeric uppercase)
- Stores in memory: `code → {fingerprint, expiresAt}` (5 minute TTL)
- Code is one-time-use — consumed on successful link
- Returns the code to the CLI

```
$ hopbox link
Link code: ABCD-1234
Expires in 5 minutes. On your other machine, connect and enter this code.
```

## Registration Flow Change

**New wizard step 0 (stepChoice):** Select field — "Create new account" / "Link to existing account"

**Create new account:** existing flow (username → wizard → container)

**Link to existing account:**
- Text input prompt for the link code
- hopboxd validates: code exists, not expired, not already used
- Links the new SSH key fingerprint to the existing user (via symlink)
- If user has 1 box → connect directly
- If user has multiple boxes → show picker
- Wizard is skipped entirely (inherits existing profile/boxes)

## Data Model — Symlink Approach

No new storage format. Linking creates a filesystem symlink:

```
data/users/
├── SHA256_original_key.../     # real directory
│   ├── user.toml
│   ├── profile.toml
│   └── boxes/
└── SHA256_new_key... → SHA256_original_key.../   # symlink
```

`store.Load()` already scans directories — symlinks resolve transparently. `LookupByFingerprint` works for both the real dir and the symlink because `os.ReadDir` follows symlinks.

**Linking implementation:** When a link code is validated, hopboxd creates a symlink:
```go
os.Symlink(originalFPDir, newFPDir)
```

Then reloads the store to pick up the new entry.

## Link Code Store

In-memory map on the Manager (or a standalone struct):

```go
type LinkCode struct {
    Fingerprint string
    ExpiresAt   time.Time
}

type LinkStore struct {
    mu    sync.Mutex
    codes map[string]LinkCode  // code -> LinkCode
}
```

- `GenerateCode(fingerprint) string` — generates code, stores with 5 min TTL
- `ValidateCode(code) (fingerprint, error)` — validates and consumes (one-time use)
- Codes are cleaned up on access (check expiry when validating)

## Control Socket — Link Command

New command in `HandleRequest`:

```json
{"command": "link"}
```

Response:
```json
{"ok": true, "data": {"code": "ABCD-1234"}}
```

The handler calls `linkStore.GenerateCode(info.Fingerprint)` — the fingerprint comes from `BoxInfo` which identifies the user's original key.

Wait — `BoxInfo.ContainerID` identifies the container, but we need the user's fingerprint to link to. The fingerprint isn't currently in `BoxInfo`. We need to add it, or resolve it from the username.

**Solution:** Add `Fingerprint string` to `BoxInfo`. It's set in `server.go` where we already have `fp`. This way the link handler knows which fingerprint to associate the code with.

## File Changes

```
internal/control/linkcodes.go   # LinkStore with GenerateCode/ValidateCode
internal/control/handler.go     # add "link" command, add Fingerprint to BoxInfo
cmd/hopbox/main.go              # add "hopbox link" command
internal/wizard/wizard.go       # add stepChoice before stepUsername
internal/users/store.go         # LinkKey method (create symlink + reload)
internal/gateway/server.go      # pass fingerprint in BoxInfo, handle link validation
internal/gateway/tunnel.go      # pass fingerprint in BoxInfo
```

## What This Does NOT Include

- Unlinking keys (future — can be done via admin UI or `hopbox unlink`)
- Listing linked keys
- Multiple users sharing a box (this is same-user multi-device only)
