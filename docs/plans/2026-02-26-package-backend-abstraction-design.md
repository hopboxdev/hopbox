# Package Backend Abstraction Design

**Goal:** Replace the duplicated switch-case dispatch in `internal/packages` with
a `Backend` interface and typed `BackendType` constants. Split the monolithic
`packages.go` into focused per-backend files.

**Scope:** Refactor only. No new features, no lock file. Public API stays the
same so existing tests pass. State file (`installed-packages.json`) is unchanged.

## Core Types

```go
type BackendType int

const (
    Apt    BackendType = iota
    Nix
    Static
)
```

`BackendType` has:
- `String() string` — returns `"apt"`, `"nix"`, `"static"`
- `ParseBackendType(s string) (BackendType, error)` — parses from string, empty
  defaults to `Apt`
- `MarshalJSON` / `UnmarshalJSON` — serializes as string for state file
  backward compatibility

```go
type Backend interface {
    Install(ctx context.Context, pkg Package) error
    IsInstalled(ctx context.Context, pkg Package) (bool, error)
    Remove(ctx context.Context, pkg Package) error
}
```

## Package Struct Change

```go
type Package struct {
    Name    string      `json:"name"`
    Backend BackendType `json:"backend,omitempty"`
    Version string      `json:"version,omitempty"`
    URL     string      `json:"url,omitempty"`
    SHA256  string      `json:"sha256,omitempty"`
}
```

`BackendType` zero value is `Apt`, preserving the current default.

## Registry

Package-level variable in `backend.go`:

```go
var backends = map[BackendType]Backend{
    Apt:    aptBackend{},
    Nix:    nixBackend{},
    Static: staticBackend{},
}
```

Public functions look up the backend and delegate:

```go
func Install(ctx context.Context, pkg Package) error {
    b, ok := backends[pkg.Backend]
    if !ok {
        return fmt.Errorf("unknown package backend %q", pkg.Backend)
    }
    return b.Install(ctx, pkg)
}
```

Same pattern for `IsInstalled` and `Remove`. Three switch statements become
three one-line lookups.

## File Structure

```
internal/packages/
  backend.go       — BackendType, Backend interface, registry, public dispatch
  apt.go           — aptBackend implementing Backend
  nix.go           — nixBackend implementing Backend
  static.go        — staticBackend + download/extract helpers
  state.go         — LoadState/SaveState (unchanged)
  packages.go      — Package struct, Reconcile, pkgKey (slimmed down)
  packages_test.go — unchanged (tests public API)
```

## Manifest Boundary

`manifest.Package` keeps `Backend string` for YAML parsing. The conversion in
`internal/agent/agent.go` calls `packages.ParseBackendType(p.Backend)` when
building `packages.Package` structs. This is the single parsing boundary.

## What Doesn't Change

- State file format and path
- Public API signatures (`Install`, `IsInstalled`, `Remove`, `Reconcile`)
- Test file (tests the public API, not internals)
- `manifest.Package` struct (stays string-based for YAML)
- `StaticBinDir` variable (still used by static backend)
