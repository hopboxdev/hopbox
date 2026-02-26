# Package Reconciliation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Auto-remove stale packages when they disappear from `hopbox.yaml`, integrated into `workspace.sync`.

**Architecture:** A JSON state file (`/etc/hopbox/installed-packages.json`) tracks what the agent installed. On every `workspace.sync`, a new `packages.Reconcile` function diffs the state file against the manifest, installs new packages, removes stale ones, and updates the state file. This runs inside `Agent.Reload`'s background goroutine, before services start (since services may depend on packages). The client's separate `packages.install` TUI step is removed — sync now covers everything.

**Tech Stack:** Go standard library (`encoding/json`, `os`, `os/exec`). No new dependencies.

**Key timeout note:** `rpcclient.Call` has a 5-second timeout. `Reload` already starts services in a `go func()`, so the RPC handler returns immediately. Package reconciliation runs inside that same goroutine (packages first, services second). No timeout change needed.

---

### Task 1: State file types and I/O

**Files:**
- Create: `internal/packages/state.go`
- Test: `internal/packages/state_test.go`

**Step 1: Write the failing tests**

```go
// internal/packages/state_test.go
package packages_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/packages"
)

func TestStateLoad_Missing(t *testing.T) {
	pkgs, err := packages.LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadState on missing file: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected empty slice, got %d packages", len(pkgs))
	}
}

func TestStateSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := []packages.Package{
		{Name: "htop", Backend: "apt"},
		{Name: "ripgrep", Backend: "static", URL: "https://example.com/rg.tar.gz"},
		{Name: "nodejs", Backend: "nix", Version: "20"},
	}
	if err := packages.SaveState(path, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := packages.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Backend != want[i].Backend || got[i].Version != want[i].Version {
			t.Errorf("package %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStateSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write initial state.
	if err := packages.SaveState(path, []packages.Package{{Name: "a"}}); err != nil {
		t.Fatal(err)
	}
	// Overwrite — should not corrupt if the process is interrupted.
	if err := packages.SaveState(path, []packages.Package{{Name: "b"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := packages.LoadState(path)
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("expected [b], got %+v", got)
	}
	// No leftover .tmp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected file: %s", e.Name())
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/packages/... -run 'TestState' -v`
Expected: compilation error — `packages.LoadState` and `packages.SaveState` undefined

**Step 3: Write minimal implementation**

```go
// internal/packages/state.go
package packages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// StatePath is the default location for the installed-packages state file.
// Variable so tests can override it.
var StatePath = "/etc/hopbox/installed-packages.json"

// stateFile is the JSON envelope for the state file.
type stateFile struct {
	Packages []Package `json:"packages"`
}

// LoadState reads the installed-packages state file.
// Returns an empty slice (not an error) if the file does not exist.
func LoadState(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	return sf.Packages, nil
}

// SaveState writes the package list to the state file atomically.
func SaveState(path string, pkgs []Package) error {
	sf := stateFile{Packages: pkgs}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/packages/... -run 'TestState' -v`
Expected: all 3 tests PASS

**Step 5: Commit**

```bash
git add internal/packages/state.go internal/packages/state_test.go
git commit -m "feat: add state file I/O for package reconciliation"
```

---

### Task 2: Remove functions per backend

**Files:**
- Modify: `internal/packages/packages.go`
- Modify: `internal/packages/packages_test.go`

**Step 1: Write the failing tests**

Add to `internal/packages/packages_test.go`:

```go
func TestRemove_Apt(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "curl", Backend: "apt"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "remove -y curl" {
		t.Errorf("apt-get args = %q, want %q", got, "remove -y curl")
	}
}

func TestRemove_AptDefault(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "apt-get", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "curl"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "remove -y curl" {
		t.Errorf("apt-get args = %q, want %q", got, "remove -y curl")
	}
}

func TestRemove_Nix(t *testing.T) {
	dir := t.TempDir()
	af := fakeBin(t, dir, "nix", "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := packages.Remove(context.Background(), packages.Package{Name: "ripgrep", Backend: "nix"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readArgs(t, af); got != "profile remove nixpkgs#ripgrep" {
		t.Errorf("nix args = %q, want %q", got, "profile remove nixpkgs#ripgrep")
	}
}

func TestRemove_Static(t *testing.T) {
	tmpDir := t.TempDir()
	packages.StaticBinDir = tmpDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Simulate a previously installed static package.
	metaDir := filepath.Join(tmpDir, ".pkg")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "ripgrep"), []byte("rg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rg"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := packages.Remove(context.Background(), packages.Package{Name: "ripgrep", Backend: "static"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Binary and metadata should be gone.
	if _, err := os.Stat(filepath.Join(tmpDir, "rg")); !os.IsNotExist(err) {
		t.Error("binary should be removed")
	}
	if _, err := os.Stat(filepath.Join(metaDir, "ripgrep")); !os.IsNotExist(err) {
		t.Error("metadata should be removed")
	}
}

func TestRemove_UnknownBackend(t *testing.T) {
	err := packages.Remove(context.Background(), packages.Package{Name: "tool", Backend: "brew"})
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/packages/... -run 'TestRemove' -v`
Expected: compilation error — `packages.Remove` undefined

**Step 3: Write minimal implementation**

Add to `internal/packages/packages.go`:

```go
// Remove removes pkg using the appropriate backend.
func Remove(ctx context.Context, pkg Package) error {
	switch pkg.Backend {
	case "apt", "":
		return aptRemove(ctx, pkg)
	case "nix":
		return nixRemove(ctx, pkg)
	case "static":
		return staticRemove(pkg)
	default:
		return fmt.Errorf("unknown package backend %q", pkg.Backend)
	}
}

func aptRemove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "apt-get", "remove", "-y", pkg.Name)
	cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func nixRemove(ctx context.Context, pkg Package) error {
	cmd := exec.CommandContext(ctx, "nix", "profile", "remove", "nixpkgs#"+pkg.Name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix profile remove %q: %w\n%s", pkg.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func staticRemove(pkg Package) error {
	binName, err := readStaticMeta(pkg.Name)
	if err != nil {
		return nil // no metadata = nothing to remove
	}
	_ = os.Remove(filepath.Join(StaticBinDir, binName))
	_ = os.Remove(filepath.Join(staticMetaDir(), pkg.Name))
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/packages/... -run 'TestRemove' -v`
Expected: all 5 tests PASS

**Step 5: Commit**

```bash
git add internal/packages/packages.go internal/packages/packages_test.go
git commit -m "feat: add Remove function for all package backends"
```

---

### Task 3: Reconcile function

**Files:**
- Modify: `internal/packages/packages.go`
- Modify: `internal/packages/packages_test.go`

**Step 1: Write the failing tests**

Add to `internal/packages/packages_test.go`:

```go
func TestReconcile_FirstRun(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	desired := []packages.Package{
		{Name: "curl", Backend: "apt"},
	}

	if err := packages.Reconcile(context.Background(), statePath, desired); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// State file should now list curl.
	got, _ := packages.LoadState(statePath)
	if len(got) != 1 || got[0].Name != "curl" {
		t.Errorf("state = %+v, want [curl]", got)
	}
}

func TestReconcile_RemoveStale(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")

	// Pretend curl and htop were previously installed.
	prev := []packages.Package{
		{Name: "curl", Backend: "apt"},
		{Name: "htop", Backend: "apt"},
	}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// New manifest only has curl.
	desired := []packages.Package{
		{Name: "curl", Backend: "apt"},
	}
	if err := packages.Reconcile(context.Background(), statePath, desired); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, _ := packages.LoadState(statePath)
	if len(got) != 1 || got[0].Name != "curl" {
		t.Errorf("state = %+v, want [curl]", got)
	}
}

func TestReconcile_EmptyManifest(t *testing.T) {
	dir := t.TempDir()
	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, fakeDir, "apt-get", "", 0)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	prev := []packages.Package{{Name: "curl", Backend: "apt"}}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// Empty manifest = remove everything.
	if err := packages.Reconcile(context.Background(), statePath, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, _ := packages.LoadState(statePath)
	if len(got) != 0 {
		t.Errorf("state = %+v, want empty", got)
	}
}

func TestReconcile_SkipUnchanged(t *testing.T) {
	dir := t.TempDir()
	fakeDir := filepath.Join(dir, "fakes")
	if err := os.MkdirAll(fakeDir, 0755); err != nil {
		t.Fatal(err)
	}
	// apt-get should NOT be called if packages are unchanged.
	// Use exit code 1 so the test fails if it's called.
	fakeBin(t, fakeDir, "apt-get", "", 1)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	statePath := filepath.Join(dir, "state.json")
	pkgs := []packages.Package{{Name: "curl", Backend: "apt"}}
	if err := packages.SaveState(statePath, pkgs); err != nil {
		t.Fatal(err)
	}

	// Same list — nothing to install or remove.
	if err := packages.Reconcile(context.Background(), statePath, pkgs); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func TestReconcile_StaticRemove(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	packages.StaticBinDir = binDir
	t.Cleanup(func() { packages.StaticBinDir = "/opt/hopbox/bin" })

	// Simulate previously installed static package.
	metaDir := filepath.Join(binDir, ".pkg")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "ripgrep"), []byte("rg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "rg"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(dir, "state.json")
	prev := []packages.Package{{Name: "ripgrep", Backend: "static", URL: "https://example.com/rg.tar.gz"}}
	if err := packages.SaveState(statePath, prev); err != nil {
		t.Fatal(err)
	}

	// Empty manifest — ripgrep should be removed.
	if err := packages.Reconcile(context.Background(), statePath, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(binDir, "rg")); !os.IsNotExist(err) {
		t.Error("binary should be removed")
	}
	got, _ := packages.LoadState(statePath)
	if len(got) != 0 {
		t.Errorf("state = %+v, want empty", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/packages/... -run 'TestReconcile' -v`
Expected: compilation error — `packages.Reconcile` undefined

**Step 3: Write minimal implementation**

Add to `internal/packages/packages.go`:

```go
// Reconcile compares the desired package list against the state file and
// installs new packages, removes stale ones, and updates the state file.
// Errors from individual installs/removes are logged but do not stop the process.
func Reconcile(ctx context.Context, statePath string, desired []Package) error {
	previous, err := LoadState(statePath)
	if err != nil {
		slog.Warn("load package state", "err", err)
		previous = nil
	}

	prevMap := make(map[string]Package, len(previous))
	for _, p := range previous {
		prevMap[pkgKey(p)] = p
	}

	desiredMap := make(map[string]Package, len(desired))
	for _, p := range desired {
		desiredMap[pkgKey(p)] = p
	}

	// Remove stale packages (in previous but not in desired).
	for key, pkg := range prevMap {
		if _, ok := desiredMap[key]; !ok {
			slog.Info("removing stale package", "name", pkg.Name, "backend", pkg.Backend)
			if err := Remove(ctx, pkg); err != nil {
				slog.Warn("remove package", "name", pkg.Name, "err", err)
			}
		}
	}

	// Install new packages (in desired but not in previous).
	var installed []Package
	for _, pkg := range desired {
		if _, ok := prevMap[pkgKey(pkg)]; ok {
			installed = append(installed, pkg) // unchanged, keep in state
			continue
		}
		slog.Info("installing package", "name", pkg.Name, "backend", pkg.Backend)
		if err := Install(ctx, pkg); err != nil {
			slog.Warn("install package", "name", pkg.Name, "err", err)
			continue // don't add to state if install failed
		}
		installed = append(installed, pkg)
	}

	if err := SaveState(statePath, installed); err != nil {
		slog.Warn("save package state", "err", err)
	}
	return nil
}

// pkgKey returns a unique key for a package based on name and backend.
func pkgKey(p Package) string {
	b := p.Backend
	if b == "" {
		b = "apt"
	}
	return b + ":" + p.Name
}
```

Note: add `"log/slog"` to the imports in `packages.go`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/packages/... -run 'TestReconcile' -v`
Expected: all 5 tests PASS

**Step 5: Run full test suite**

Run: `go test ./internal/packages/... -v`
Expected: all tests PASS (existing + new)

**Step 6: Commit**

```bash
git add internal/packages/packages.go internal/packages/packages_test.go
git commit -m "feat: add Reconcile function for package state convergence"
```

---

### Task 4: Integrate into Agent.Reload

**Files:**
- Modify: `internal/agent/agent.go:65-95` (the `Reload` method)

**Step 1: Update Reload to call Reconcile**

Change `Reload` to run package reconciliation before starting services:

```go
// Reload re-wires the agent's runtime state from the given workspace manifest.
// Called after workspace.sync is received. Reconciles packages, rebuilds the
// service manager, stops old services, and starts fresh ones.
func (a *Agent) Reload(ws *manifest.Workspace) {
	mgr := BuildServiceManager(ws)

	a.mu.Lock()
	old := a.services
	a.scripts = ws.Scripts
	if ws.Backup != nil {
		a.backupTarget = ws.Backup.Target
		a.backupPaths = mgr.DataPaths()
	} else {
		a.backupTarget = ""
		a.backupPaths = nil
	}
	a.services = mgr
	a.mu.Unlock()

	if err := InstallBridgeScripts(ws); err != nil {
		slog.Warn("install bridge scripts", "err", err)
	}

	go func() {
		// Reconcile packages before starting services (services may depend on packages).
		pkgs := make([]packages.Package, len(ws.Packages))
		for i, p := range ws.Packages {
			pkgs[i] = packages.Package{
				Name:    p.Name,
				Backend: p.Backend,
				Version: p.Version,
				URL:     p.URL,
				SHA256:  p.SHA256,
			}
		}
		if err := packages.Reconcile(context.Background(), packages.StatePath, pkgs); err != nil {
			slog.Warn("package reconciliation", "err", err)
		}

		if old != nil {
			if err := old.StopAll(); err != nil {
				slog.Warn("stop old services on reload", "err", err)
			}
		}
		if err := mgr.StartAll(context.Background()); err != nil {
			slog.Warn("service startup on reload", "err", err)
		}
	}()
}
```

Note: add `"github.com/hopboxdev/hopbox/internal/packages"` to imports in `agent.go`.

**Step 2: Check if manifest.Package and packages.Package are the same type**

The manifest package defines its own `Package` struct. Check if we need a conversion or if they're the same type. If `manifest.Package` and `packages.Package` have the same fields, we may need the explicit conversion shown above. If they're the same type (or one is an alias), simplify.

Run: `grep -n 'type Package struct' internal/manifest/manifest.go`

If they're different structs with the same fields, the conversion in the code above is correct.

**Step 3: Run tests**

Run: `go test ./internal/agent/... -v`
Expected: PASS (existing tests should still pass; Reload is called in tests but package reconciliation will be a no-op with no state file)

**Step 4: Run linter**

Run: `golangci-lint run ./internal/agent/...`
Expected: clean

**Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: run package reconciliation in workspace.sync Reload"
```

---

### Task 5: Remove packages.install TUI step from client

**Files:**
- Modify: `cmd/hop/up.go:489-526`

**Step 1: Edit up.go**

Replace lines 489-526 (the sync step + packages step) with a single combined step:

```go
		wsSteps = append(wsSteps, tui.Step{
			Title: fmt.Sprintf("Syncing workspace: %s", ws.Name),
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				yamlBytes, err := yaml.Marshal(ws)
				if err != nil {
					return fmt.Errorf("marshal manifest: %w", err)
				}
				if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(yamlBytes)}); err != nil {
					return fmt.Errorf("workspace sync: %w", err)
				}
				send(tui.StepEvent{Message: "Workspace synced"})
				return nil
			},
			NonFatal: true,
		})
```

This removes the entire `if len(ws.Packages) > 0 { ... }` block (lines 504-526) and renames "Syncing manifest" to "Syncing workspace".

**Step 2: Run tests**

Run: `go test ./cmd/hop/... -v`
Expected: PASS

**Step 3: Build**

Run: `make build`
Expected: clean build

**Step 4: Commit**

```bash
git add cmd/hop/up.go
git commit -m "refactor: remove packages.install TUI step, sync now covers packages"
```

---

### Task 6: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark package reconciliation as complete**

Change: `- [ ] Package reconciliation — remove stale binaries/packages not in current manifest`
To: `- [x] Package reconciliation — remove stale binaries/packages not in current manifest`

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark package reconciliation as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `internal/packages/state.go` | Create — state file types + Load/Save |
| `internal/packages/state_test.go` | Create — state file tests |
| `internal/packages/packages.go` | Modify — add `Remove`, `Reconcile`, `pkgKey` |
| `internal/packages/packages_test.go` | Modify — add Remove + Reconcile tests |
| `internal/agent/agent.go` | Modify — call `packages.Reconcile` in `Reload` |
| `cmd/hop/up.go` | Modify — remove packages.install step, rename sync step |
| `ROADMAP.md` | Modify — check off package reconciliation |
