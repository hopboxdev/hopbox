# Dotenv Secrets Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Load `.env` and `.env.local` files on the client, merge into workspace env, and apply workspace-level env to all services on the agent.

**Architecture:** Client-side .env parsing in a new `internal/dotenv` package. `BuildServiceManager` fixed to merge `Workspace.Env` into each service's env. `hop up` reads .env files before `workspace.sync`. Remove unused `Secret` struct.

**Tech Stack:** Go standard library only (no external dotenv deps).

---

### Task 1: Create dotenv parser with tests

**Files:**
- Create: `internal/dotenv/dotenv.go`
- Create: `internal/dotenv/dotenv_test.go`

**Step 1: Write the failing test**

Create `internal/dotenv/dotenv_test.go`:

```go
package dotenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/dotenv"
)

func TestParseBasicKeyValue(t *testing.T) {
	env, err := dotenv.ParseString("FOO=bar\nBAZ=qux")
	if err != nil {
		t.Fatal(err)
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
	if env["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", env["BAZ"], "qux")
	}
}

func TestParseCommentsAndBlanks(t *testing.T) {
	input := "# this is a comment\nFOO=bar\n\n# another\nBAZ=qux\n"
	env, err := dotenv.ParseString(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 {
		t.Errorf("len = %d, want 2", len(env))
	}
}

func TestParseDoubleQuoted(t *testing.T) {
	env, err := dotenv.ParseString(`KEY="hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "hello world" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "hello world")
	}
}

func TestParseSingleQuoted(t *testing.T) {
	env, err := dotenv.ParseString(`KEY='literal $value'`)
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "literal $value" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "literal $value")
	}
}

func TestParseExportPrefix(t *testing.T) {
	env, err := dotenv.ParseString("export FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseEmptyValue(t *testing.T) {
	env, err := dotenv.ParseString("KEY=")
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "" {
		t.Errorf("KEY = %q, want empty", env["KEY"])
	}
}

func TestParseValueWithEquals(t *testing.T) {
	env, err := dotenv.ParseString("URL=postgres://localhost/db?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if env["URL"] != "postgres://localhost/db?sslmode=disable" {
		t.Errorf("URL = %q, want full URL", env["URL"])
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\nB=2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	env, err := dotenv.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Errorf("env = %v", env)
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := dotenv.ParseFile("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\nB=2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("B=override\nC=3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	env, n, err := dotenv.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" {
		t.Errorf("A = %q, want %q", env["A"], "1")
	}
	if env["B"] != "override" {
		t.Errorf("B = %q, want %q (should be overridden by .env.local)", env["B"], "override")
	}
	if env["C"] != "3" {
		t.Errorf("C = %q, want %q", env["C"], "3")
	}
	if n != 3 {
		t.Errorf("n = %d, want 3 (total unique vars loaded)", n)
	}
}

func TestLoadDirNoFiles(t *testing.T) {
	dir := t.TempDir()
	env, n, err := dotenv.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 0 || n != 0 {
		t.Errorf("expected empty env, got %v (n=%d)", env, n)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dotenv/...`
Expected: Compilation error — package doesn't exist yet.

**Step 3: Write minimal implementation**

Create `internal/dotenv/dotenv.go`:

```go
// Package dotenv parses .env files into key-value maps.
package dotenv

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseString parses dotenv-formatted text into a map.
func ParseString(s string) (map[string]string, error) {
	env := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := line[idx+1:]
		val = unquote(val)
		env[key] = val
	}
	return env, scanner.Err()
}

// unquote removes surrounding double or single quotes from a value.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ParseFile reads and parses a .env file.
func ParseFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseString(string(data))
}

// LoadDir loads .env and .env.local from dir, merging them (.env.local wins).
// Returns the merged map and the total number of unique variables loaded.
// Missing files are silently skipped.
func LoadDir(dir string) (map[string]string, int, error) {
	merged := make(map[string]string)
	for _, name := range []string{".env", ".env.local"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		env, err := ParseFile(path)
		if err != nil {
			return nil, 0, err
		}
		for k, v := range env {
			merged[k] = v
		}
	}
	return merged, len(merged), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/dotenv/... -v`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/dotenv/dotenv.go internal/dotenv/dotenv_test.go
git commit -m "feat: add dotenv parser for .env file loading"
```

---

### Task 2: Fix BuildServiceManager to apply Workspace.Env

**Files:**
- Modify: `internal/agent/services.go:16-95`
- Create test in: `internal/agent/services_test.go`

**Step 1: Write the failing test**

Create `internal/agent/services_test.go`:

```go
package agent_test

import (
	"testing"

	"github.com/hopboxdev/hopbox/internal/agent"
	"github.com/hopboxdev/hopbox/internal/manifest"
)

func TestBuildServiceManagerMergesWorkspaceEnv(t *testing.T) {
	ws := &manifest.Workspace{
		Name: "test",
		Env: map[string]string{
			"GLOBAL_A": "from-workspace",
			"SHARED":   "workspace-level",
		},
		Services: map[string]manifest.Service{
			"api": {
				Type:    "native",
				Command: "./server",
				Env: map[string]string{
					"SHARED":    "service-level",
					"SERVICE_B": "only-in-service",
				},
			},
			"worker": {
				Type:    "native",
				Command: "./worker",
				// No service-level env — should inherit workspace env.
			},
		},
	}

	mgr := agent.BuildServiceManager(ws)
	statuses := mgr.ListStatus()

	// Build a lookup by name.
	defByName := map[string]map[string]string{}
	for _, s := range statuses {
		defByName[s.Name] = mgr.Def(s.Name).Env
	}

	// api: SHARED should be "service-level" (service wins), GLOBAL_A inherited.
	apiEnv := defByName["api"]
	if apiEnv["GLOBAL_A"] != "from-workspace" {
		t.Errorf("api GLOBAL_A = %q, want %q", apiEnv["GLOBAL_A"], "from-workspace")
	}
	if apiEnv["SHARED"] != "service-level" {
		t.Errorf("api SHARED = %q, want %q", apiEnv["SHARED"], "service-level")
	}
	if apiEnv["SERVICE_B"] != "only-in-service" {
		t.Errorf("api SERVICE_B = %q, want %q", apiEnv["SERVICE_B"], "only-in-service")
	}

	// worker: should get workspace env only.
	workerEnv := defByName["worker"]
	if workerEnv["GLOBAL_A"] != "from-workspace" {
		t.Errorf("worker GLOBAL_A = %q, want %q", workerEnv["GLOBAL_A"], "from-workspace")
	}
	if workerEnv["SHARED"] != "workspace-level" {
		t.Errorf("worker SHARED = %q, want %q", workerEnv["SHARED"], "workspace-level")
	}
}

func TestBuildServiceManagerNoWorkspaceEnv(t *testing.T) {
	ws := &manifest.Workspace{
		Name: "test",
		Services: map[string]manifest.Service{
			"api": {
				Type:    "native",
				Command: "./server",
				Env:     map[string]string{"KEY": "val"},
			},
		},
	}
	mgr := agent.BuildServiceManager(ws)
	env := mgr.Def("api").Env
	if env["KEY"] != "val" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "val")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestBuildServiceManager -v`
Expected: FAIL — `mgr.Def` method doesn't exist yet, and workspace env isn't merged.

**Step 3: Add Def accessor to Manager**

The test needs `mgr.Def(name)` to inspect the service definition. Add to `internal/service/manager.go`:

```go
// Def returns the service definition for the given name, or nil.
func (m *Manager) Def(name string) *Def {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return nil
	}
	return e.def
}
```

Find the existing `Backend` accessor method in `manager.go` and add `Def` right next to it using the same pattern.

**Step 4: Update BuildServiceManager to merge workspace env**

In `internal/agent/services.go`, add env merging at the start of the `for name, svc` loop (before creating `def`):

```go
// Merge workspace-level env into service env.
// Service-level values take precedence.
mergedEnv := make(map[string]string, len(ws.Env)+len(svc.Env))
for k, v := range ws.Env {
	mergedEnv[k] = v
}
for k, v := range svc.Env {
	mergedEnv[k] = v
}
```

Then use `mergedEnv` instead of `svc.Env` in three places:
1. `def.Env: mergedEnv` (line ~48)
2. `DockerBackend{Env: mergedEnv}` (line ~76)
3. `NativeBackend{Env: mergedEnv}` (line ~87)

**Step 5: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestBuildServiceManager -v`
Expected: All PASS.

**Step 6: Run full test suite**

Run: `go test ./...`
Expected: All PASS — existing tests should not break.

**Step 7: Commit**

```bash
git add internal/agent/services.go internal/agent/services_test.go internal/service/manager.go
git commit -m "feat: merge workspace-level env into all services"
```

---

### Task 3: Load .env files in hop up before workspace.sync

**Files:**
- Modify: `cmd/hop/up.go:440-511` (buildTUIPhases)
- Modify: `cmd/hop/up.go:199-210` (foreground manifest load)
- Modify: `cmd/hop/up.go:369-380` (daemon manifest load)

**Step 1: Add dotenv loading helper**

Add a helper function at the bottom of `cmd/hop/up.go`:

```go
// loadDotenv reads .env and .env.local from the same directory as wsPath,
// merges them into ws.Env (manifest values take precedence), and returns
// a summary message. If no .env files exist, ws is unchanged.
func loadDotenv(ws *manifest.Workspace, wsPath string) string {
	dir := filepath.Dir(wsPath)
	envVars, n, err := dotenv.LoadDir(dir)
	if err != nil {
		return fmt.Sprintf("Warning: %v", err)
	}
	if n == 0 {
		return ""
	}
	// Merge: .env values are base, manifest Env wins.
	merged := make(map[string]string, len(envVars)+len(ws.Env))
	for k, v := range envVars {
		merged[k] = v
	}
	for k, v := range ws.Env {
		merged[k] = v
	}
	ws.Env = merged
	return fmt.Sprintf("Loaded %d var(s) from .env files", n)
}
```

Add `"github.com/hopboxdev/hopbox/internal/dotenv"` to the imports.

**Step 2: Call loadDotenv in buildTUIPhases**

In `buildTUIPhases`, inside the `if ws != nil` block (around line 467), before the manifest sync step, add:

```go
if msg := loadDotenv(ws, wsPath); msg != "" {
	wsSteps = append(wsSteps, tui.Step{
		Title: msg,
		Run: func(_ context.Context, _ func(tui.StepEvent)) error {
			return nil
		},
	})
}
```

**Step 3: Re-marshal manifest with merged env for sync**

The manifest sync step currently reads the raw file with `os.ReadFile(wsPath)`. After merging .env into `ws.Env`, we need to send the *modified* manifest. Change the sync step's Run function to marshal `ws` instead of re-reading the file:

Replace the sync step's body. Instead of `rawManifest, err := os.ReadFile(wsPath)`, use `yaml.Marshal(ws)`:

```go
wsSteps = append(wsSteps, tui.Step{
	Title: fmt.Sprintf("Syncing manifest: %s", ws.Name),
	Run: func(ctx context.Context, send func(tui.StepEvent)) error {
		yamlBytes, err := yaml.Marshal(ws)
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
		if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(yamlBytes)}); err != nil {
			return fmt.Errorf("manifest sync: %w", err)
		}
		send(tui.StepEvent{Message: "Manifest synced"})
		return nil
	},
	NonFatal: true,
})
```

Add `"gopkg.in/yaml.v3"` to imports.

**Step 4: Apply same pattern in foreground and daemon paths**

Both `runForeground` (line ~199-210) and `runTUIPhases` (line ~369-380) load the manifest. After parsing, call `loadDotenv(ws, wsPath)` in each location (only if `ws != nil`). The buildTUIPhases function is shared so the sync step change covers both paths.

In `runForeground`, after line 209 (`ws, err = manifest.Parse(wsPath)`), add:
```go
if ws != nil {
	if msg := loadDotenv(ws, wsPath); msg != "" {
		fmt.Println(ui.StepInfo(msg))
	}
}
```

In `runTUIPhases`, after line 379 (`ws, err = manifest.Parse(wsPath)`), add the same block.

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: All PASS.

**Step 6: Commit**

```bash
git add cmd/hop/up.go
git commit -m "feat: load .env files during hop up before workspace sync"
```

---

### Task 4: Remove Secret struct and secrets field

**Files:**
- Modify: `internal/manifest/manifest.go:11-23,65-70`
- Modify: `internal/manifest/manifest_test.go` (if any test references secrets)

**Step 1: Check for Secret usage across codebase**

Run: `grep -r "Secret\|secrets" internal/ cmd/ --include="*.go" -l`

This identifies all files referencing Secret or secrets.

**Step 2: Remove Secret struct and Secrets field**

In `internal/manifest/manifest.go`:
- Remove `Secrets []Secret` from `Workspace` struct (line 18)
- Remove the `Secret` struct definition (lines 65-70)

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All PASS. The example YAML in tests doesn't use `secrets:`, and YAML unmarshalling silently ignores unknown fields, so removing the struct is safe.

**Step 4: Commit**

```bash
git add internal/manifest/manifest.go
git commit -m "refactor: remove unused Secret struct, replaced by .env files"
```

---

### Task 5: Verify end-to-end and run linter

**Files:** None (verification only).

**Step 1: Run full test suite**

Run: `go test ./...`
Expected: All PASS.

**Step 2: Run linter**

Run: `golangci-lint run`
Expected: No new issues.

**Step 3: Build all binaries**

Run: `make build`
Expected: Clean build.

**Step 4: Commit (if any lint fixes needed)**

Fix any lint issues and commit:
```bash
git add -A && git commit -m "fix: address lint issues from dotenv implementation"
```

---

### Task 6: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark secrets management as complete**

In `ROADMAP.md`, change:
```
- [ ] Secrets management — sops/age integration, `hop secret set`
```
to:
```
- [x] Secrets management — .env file loading, workspace env merge
```

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark secrets management as complete in roadmap"
```
