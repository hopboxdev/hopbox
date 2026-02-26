# Devcontainer Import Design

## Goal

One-way migration tool: `hop init --from .devcontainer/devcontainer.json` reads a devcontainer.json and generates a `hopbox.yaml` with best-effort field mapping. Targets users migrating existing projects to hopbox.

## Field Mapping

| devcontainer.json | hopbox.yaml | Notes |
|---|---|---|
| `image` | Comment + inferred packages | Parse image name for runtime hints (e.g. `devcontainers/go:1.22` -> `{name: go, backend: apt}`) |
| `features` | `packages:` | Lookup table for common features. Unknown features -> YAML comment |
| `forwardPorts` | `services[0].ports:` or comment | Ports without a service noted as comments |
| `containerEnv` | `env:` | Direct 1:1 map |
| `postCreateCommand` | `scripts.setup:` | Stored as named script |
| `postStartCommand` | `scripts.start:` | Stored as named script |
| `customizations.vscode.extensions` | `editor.extensions:` | New `extensions` field on Editor struct |
| `mounts` | Comments | No clean mapping -- noted for manual setup |
| `dockerComposeFile` | `services:` | Parse compose YAML, map each service to hopbox service def |

**Ignored (with comments):** `remoteUser`, `build`, `runArgs`, `initializeCommand`, `shutdownAction`, `userEnvProbe`.

## Architecture

New `internal/devcontainer` package with one public function:

```go
func Convert(path string) (*manifest.Workspace, []string, error)
```

Returns the generated workspace and a list of warnings for unmapped fields.

### Feature-to-Package Lookup

Hardcoded `map[string]manifest.Package` covering ~10 common devcontainer features:

- node -> `{name: nodejs, backend: nix}`
- python -> `{name: python3, backend: apt}`
- go -> `{name: go, backend: apt}`
- rust -> `{name: rustup, backend: static, url: ...}`
- java -> `{name: openjdk, backend: apt}`
- docker-in-docker -> comment (not applicable on VPS)
- git -> `{name: git, backend: apt}`
- github-cli -> `{name: gh, backend: apt}`
- common-utils -> ignored (curl, wget, etc. already on most VPS)
- terraform -> `{name: terraform, backend: static, url: ...}`

Easy to extend by adding entries to the map.

### Docker Compose Extraction

When `dockerComposeFile` is referenced, parse the compose YAML and map each service:

- `image` -> hopbox service image
- `ports` -> hopbox service ports
- `environment` -> hopbox service env
- `volumes` -> hopbox service data mounts (host:container pairs)
- `depends_on` -> hopbox service depends_on

### Image-to-Package Inference

Parse known devcontainer image names to extract runtime hints:

- `mcr.microsoft.com/devcontainers/go:1.22` -> go package
- `mcr.microsoft.com/devcontainers/python:3.12` -> python3 package
- `mcr.microsoft.com/devcontainers/typescript-node:20` -> nodejs package
- Unknown images -> YAML comment noting the original image

## Manifest Changes

Add `Extensions []string` to `manifest.Editor`:

```go
type Editor struct {
    Type       string   `yaml:"type,omitempty"`
    Path       string   `yaml:"path,omitempty"`
    Extensions []string `yaml:"extensions,omitempty"`
}
```

## CLI Integration

Add `--from` flag to `hop init`:

```go
initCmd.Flags().StringVar(&fromFile, "from", "", "import from devcontainer.json")
```

When `--from` is set, call `devcontainer.Convert()` instead of generating the default scaffold. Marshal the result to YAML and write `hopbox.yaml`.

## Error Handling

- **JSONC support:** Strip comments and trailing commas before JSON parsing (devcontainer files commonly use JSONC).
- **Missing compose file:** If `dockerComposeFile` references a file that doesn't exist, warn and skip.
- **Existing hopbox.yaml:** Refuse and tell user to delete it first.
- **Warnings:** Printed to stderr and embedded as YAML comments in the generated file.

## What This Does Not Do

- No bidirectional sync (devcontainer.json is not updated when hopbox.yaml changes).
- No devcontainer feature registry/download (just a static lookup table).
- No Dockerfile parsing (if `build` is used instead of `image`, we warn).
- No runtime behavior changes -- this is purely a config translator.
