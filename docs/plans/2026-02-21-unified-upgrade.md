# Unified `hop upgrade` Implementation Plan

> **Status: Implemented** (2026-02-21)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the agent-only `hop upgrade` with a single command that updates all three binaries (client, helper, agent) from GitHub releases or local dev builds.

**Architecture:** Export download utilities from `internal/setup`, add package manager detection to `internal/version`, add a version query to the helper daemon protocol, add hop-helper to goreleaser, then rewrite the upgrade command to orchestrate all three component updates with `--local`, `--version`, and component filter flags.

**Tech Stack:** Go, goreleaser, GitHub Releases API, macOS LaunchDaemon

---

### Task 1: Export download utilities and fix checksums URL

**Files:**
- Modify: `internal/setup/install.go`

The `fetchURL` and `lookupChecksum` functions are unexported but needed by the new upgrade logic. Export them and fix the checksums URL which currently uses `hop-agent_{version}_checksums.txt` instead of goreleaser's actual output `checksums.txt`.

**Step 1: Export FetchURL and LookupChecksum, fix checksums URL**

In `internal/setup/install.go`:

1. Rename `fetchURL` → `FetchURL` (update its doc comment to start with `FetchURL`).
2. Rename `lookupChecksum` → `LookupChecksum` (update its doc comment to start with `LookupChecksum`).
3. Update callers inside `installAgent` to use the new names.
4. Fix the checksums URL in `installAgent` from:
   ```go
   csURL := fmt.Sprintf(
       "https://github.com/hopboxdev/hopbox/releases/download/v%s/hop-agent_%s_checksums.txt",
       v, v,
   )
   ```
   to:
   ```go
   csURL := fmt.Sprintf(
       "https://github.com/hopboxdev/hopbox/releases/download/v%s/checksums.txt",
       v,
   )
   ```

**Step 2: Add targetVersion parameter to installAgent**

Currently `installAgent` reads `version.Version` directly for the download URL. When upgrading to a version different from the running client, this is wrong. Add a `targetVersion` parameter:

Change signature from:
```go
func installAgent(ctx context.Context, client *ssh.Client, out io.Writer) error {
```
to:
```go
func installAgent(ctx context.Context, client *ssh.Client, out io.Writer, targetVersion string) error {
```

Replace the version resolution block:
```go
var data []byte

if localPath := os.Getenv("HOP_AGENT_BINARY"); localPath != "" {
    // ... (unchanged)
} else {
    v := targetVersion
    if v == "" || v == "dev" {
        return fmt.Errorf(
            "no release found for version %q; set HOP_AGENT_BINARY to a local hop-agent binary",
            v,
        )
    }
    // ... rest unchanged, uses v for binName and binURL
}
```

**Step 3: Update UpgradeAgent to pass targetVersion through**

In `internal/setup/upgrade.go`, update the call to `installAgent`:
```go
if err := installAgent(ctx, client, out, clientVersion); err != nil {
```

**Step 4: Run tests**

Run: `go test ./internal/setup/... -v`
Expected: PASS (no existing tests cover these functions directly, but compilation must succeed)

Run: `go test ./... 2>&1 | head -50`
Expected: All tests PASS (no callers broken)

**Step 5: Commit**

```bash
git add internal/setup/install.go internal/setup/upgrade.go
git commit -m "refactor: export download utilities and fix checksums URL"
```

---

### Task 2: Add PackageManager detection

**Files:**
- Modify: `internal/version/version.go`
- Create: `internal/version/pkgmanager.go`
- Create: `internal/version/pkgmanager_test.go`

**Step 1: Add PackageManager var**

In `internal/version/version.go`, add one line to the var block:

```go
var (
	Version        = "dev"
	Commit         = "unknown"
	Date           = "unknown"
	PackageManager = ""
)
```

**Step 2: Write the failing test**

Create `internal/version/pkgmanager_test.go`:

```go
package version

import "testing"

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/hopbox/0.1.0/bin/hop", "brew"},
		{"linuxbrew", "/home/user/.linuxbrew/bin/hop", "brew"},
		{"nix store", "/nix/store/abc123-hopbox/bin/hop", "nix"},
		{"standalone", "/usr/local/bin/hop", ""},
		{"go install", "/Users/user/go/bin/hop", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPackageManager(tt.path)
			if got != tt.want {
				t.Errorf("DetectPackageManager(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectPackageManager_BuildTimeOverride(t *testing.T) {
	old := PackageManager
	PackageManager = "brew"
	defer func() { PackageManager = old }()

	got := DetectPackageManager("/usr/local/bin/hop")
	if got != "brew" {
		t.Errorf("expected build-time override 'brew', got %q", got)
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/version/... -run TestDetectPackageManager -v`
Expected: FAIL — `DetectPackageManager` not defined

**Step 4: Implement DetectPackageManager**

Create `internal/version/pkgmanager.go`:

```go
package version

import "strings"

// DetectPackageManager returns the package manager name if hop was installed
// via one, or empty string if standalone. Checks the build-time PackageManager
// var first, then falls back to executable path heuristics.
func DetectPackageManager(execPath string) string {
	if PackageManager != "" {
		return PackageManager
	}
	if strings.Contains(execPath, "/Cellar/") ||
		strings.Contains(execPath, "/homebrew/") ||
		strings.Contains(execPath, "/linuxbrew/") {
		return "brew"
	}
	if strings.Contains(execPath, "/nix/store/") {
		return "nix"
	}
	return ""
}
```

**Step 5: Run tests**

Run: `go test ./internal/version/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/version/version.go internal/version/pkgmanager.go internal/version/pkgmanager_test.go
git commit -m "feat: add package manager detection for upgrade safety"
```

---

### Task 3: Add version query to helper daemon

**Files:**
- Modify: `internal/helper/protocol.go`
- Modify: `internal/helper/client.go`
- Modify: `cmd/hop-helper/main.go`

**Step 1: Add ActionVersion and Version field to protocol**

In `internal/helper/protocol.go`:

Add `ActionVersion` to the const block:
```go
const (
	ActionCreateTUN    = "create_tun"
	ActionConfigureTUN = "configure_tun"
	ActionCleanupTUN   = "cleanup_tun"
	ActionAddHost      = "add_host"
	ActionRemoveHost   = "remove_host"
	ActionVersion      = "version"
)
```

Add `Version` field to Response:
```go
type Response struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Interface string `json:"interface,omitempty"`
	Version   string `json:"version,omitempty"`
}
```

**Step 2: Add Version() method to helper client**

In `internal/helper/client.go`, add this method:

```go
// Version queries the helper daemon's version.
func (c *Client) Version() (string, error) {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("connect to helper at %s: %w", c.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Action: ActionVersion}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("helper: %s", resp.Error)
	}
	return resp.Version, nil
}
```

**Step 3: Handle ActionVersion in hop-helper daemon**

In `cmd/hop-helper/main.go`:

Add `"github.com/hopboxdev/hopbox/internal/version"` to the import block.

In the `handle` function, add a case for `ActionVersion` before the switch on `req.Action` (after the CreateTUN special case, before the switch):

```go
if req.Action == helper.ActionVersion {
	_ = json.NewEncoder(conn).Encode(helper.Response{OK: true, Version: version.Version})
	return
}
```

**Step 4: Run tests and build**

Run: `go test ./internal/helper/... -v`
Expected: PASS

Run: `make build`
Expected: All binaries build successfully (verifies hop-helper compiles with new import)

**Step 5: Commit**

```bash
git add internal/helper/protocol.go internal/helper/client.go cmd/hop-helper/main.go
git commit -m "feat: add version query to helper daemon protocol"
```

---

### Task 4: Add hop-helper to goreleaser

**Files:**
- Modify: `.goreleaser.yaml`

**Step 1: Add hop-helper build and archive**

In `.goreleaser.yaml`, add a third build entry after the `hop-agent` build:

```yaml
  - id: hop-helper
    binary: hop-helper
    main: ./cmd/hop-helper
    goos: [darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X github.com/hopboxdev/hopbox/internal/version.Version={{.Version}}
      - -X github.com/hopboxdev/hopbox/internal/version.Commit={{.Commit}}
      - -X github.com/hopboxdev/hopbox/internal/version.Date={{.Date}}
```

Add a third archive entry after `hop-agent-archive`:

```yaml
  - id: hop-helper-archive
    builds: [hop-helper]
    name_template: "{{ .Binary }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: binary
```

**Step 2: Verify with goreleaser snapshot**

Run: `goreleaser build --snapshot --clean 2>&1 | tail -20`
Expected: Output shows successful builds for hop-helper darwin/amd64 and darwin/arm64

**Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "build: add hop-helper to goreleaser releases"
```

---

### Task 5: Implement unified upgrade command

**Files:**
- Rewrite: `cmd/hop/upgrade.go`
- Modify: `cmd/hop/main.go`
- Create: `cmd/hop/upgrade_test.go`

This is the main task. The new `UpgradeCmd` orchestrates client self-update, helper update, and agent update.

**Step 1: Write failing tests**

Create `cmd/hop/upgrade_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "hop")
	if err := os.WriteFile(binPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := atomicReplace(binPath, []byte("new")); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("expected 'new', got %q", string(data))
	}

	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("expected 0755, got %o", info.Mode().Perm())
	}
}

func TestAtomicReplace_CleansUpOnFailure(t *testing.T) {
	// Non-existent directory → Rename will fail.
	err := atomicReplace("/no/such/dir/hop", []byte("data"))
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
	// The .new file should not be left behind.
	if _, statErr := os.Stat("/no/such/dir/hop.new"); statErr == nil {
		t.Fatal(".new file should have been cleaned up")
	}
}

func TestResolveLocalPaths(t *testing.T) {
	paths := resolveLocalPaths("/some/dist")
	if paths.client != "/some/dist/hop" {
		t.Errorf("client = %q, want /some/dist/hop", paths.client)
	}
	if paths.helper != "/some/dist/hop-helper" {
		t.Errorf("helper = %q, want /some/dist/hop-helper", paths.helper)
	}
	if paths.agent != "/some/dist/hop-agent-linux" {
		t.Errorf("agent = %q, want /some/dist/hop-agent-linux", paths.agent)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/hop/... -run "TestAtomicReplace|TestResolveLocalPaths" -v`
Expected: FAIL — functions not defined

**Step 3: Rewrite cmd/hop/upgrade.go**

Replace the entire file with:

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/version"
)

// UpgradeCmd upgrades hop binaries (client, helper, agent).
type UpgradeCmd struct {
	TargetVersion string `name:"version" help:"Target version (e.g. 0.3.0). Default: latest release."`
	Local         bool   `help:"Use local dev builds from ./dist/." short:"l"`
	ClientOnly    bool   `help:"Only upgrade the hop client binary."`
	AgentOnly     bool   `help:"Only upgrade the agent on the remote host."`
	HelperOnly    bool   `help:"Only upgrade the helper daemon (macOS)."`
}

type localPaths struct {
	client string
	helper string
	agent  string
}

func resolveLocalPaths(distDir string) localPaths {
	return localPaths{
		client: filepath.Join(distDir, "hop"),
		helper: filepath.Join(distDir, "hop-helper"),
		agent:  filepath.Join(distDir, "hop-agent-linux"),
	}
}

const releaseBaseURL = "https://github.com/hopboxdev/hopbox/releases/download"

func (c *UpgradeCmd) Run(globals *CLI) error {
	ctx := context.Background()

	// Determine which components to upgrade.
	doClient := !c.AgentOnly && !c.HelperOnly
	doHelper := !c.ClientOnly && !c.AgentOnly
	doAgent := !c.ClientOnly && !c.HelperOnly

	// Resolve target version.
	targetVersion := c.TargetVersion
	if !c.Local && targetVersion == "" {
		fmt.Println("Checking for latest release...")
		v, err := latestRelease(ctx)
		if err != nil {
			return fmt.Errorf("fetch latest release: %w", err)
		}
		targetVersion = v
		fmt.Printf("Latest release: %s\n\n", targetVersion)
	}

	if c.Local {
		fmt.Println("Upgrading from local builds (./dist/)...\n")
	}

	// --- Client ---
	if doClient {
		if err := c.upgradeClient(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade client: %w", err)
		}
	}

	// --- Helper (macOS only) ---
	if doHelper && runtime.GOOS == "darwin" {
		if err := c.upgradeHelper(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade helper: %w", err)
		}
	}

	// --- Agent ---
	if doAgent {
		if err := c.upgradeAgent(ctx, globals, targetVersion); err != nil {
			return fmt.Errorf("upgrade agent: %w", err)
		}
	}

	fmt.Println("\nUpgrade complete.")
	return nil
}

func (c *UpgradeCmd) upgradeClient(ctx context.Context, targetVersion string) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}

	// Check package manager.
	if pm := version.DetectPackageManager(execPath); pm != "" {
		fmt.Printf("  Client: installed via %s — run your package manager to update.\n", pm)
		return nil
	}

	// Version check (skip for --local since versions are both "dev").
	if !c.Local && targetVersion == version.Version {
		fmt.Printf("  Client: already at %s\n", version.Version)
		return nil
	}

	var data []byte
	if c.Local {
		paths := resolveLocalPaths("dist")
		data, err = os.ReadFile(paths.client)
		if err != nil {
			return fmt.Errorf("read %s: %w", paths.client, err)
		}
		fmt.Printf("  Client: upgrading from local build...")
	} else {
		binName := fmt.Sprintf("hop_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
		binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
		fmt.Printf("  Client: %s → %s ", version.Version, targetVersion)
		data, err = setup.FetchURL(ctx, binURL)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		if err := verifyChecksum(ctx, targetVersion, binName, data); err != nil {
			return err
		}
	}

	if err := atomicReplace(execPath, data); err != nil {
		return err
	}
	fmt.Printf(" done (%s)\n", execPath)
	return nil
}

func (c *UpgradeCmd) upgradeHelper(ctx context.Context, targetVersion string) error {
	helperClient := helper.NewClient()

	// Version check via helper daemon.
	if !c.Local && helperClient.IsReachable() {
		if hv, err := helperClient.Version(); err == nil && hv == targetVersion {
			fmt.Printf("  Helper: already at %s\n", hv)
			return nil
		}
	}

	var data []byte
	var err error
	if c.Local {
		paths := resolveLocalPaths("dist")
		data, err = os.ReadFile(paths.helper)
		if err != nil {
			return fmt.Errorf("read %s: %w", paths.helper, err)
		}
		fmt.Println("  Helper: upgrading from local build (requires sudo)")
	} else {
		binName := fmt.Sprintf("hop-helper_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
		binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
		fmt.Printf("  Helper: upgrading to %s (requires sudo)\n", targetVersion)
		data, err = setup.FetchURL(ctx, binURL)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		if err := verifyChecksum(ctx, targetVersion, binName, data); err != nil {
			return err
		}
	}

	// Write to temp file, then sudo --install.
	tmp, err := os.CreateTemp("", "hop-helper-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, data, 0755); err != nil {
		return err
	}

	cmd := exec.Command("sudo", tmpPath, "--install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helper --install: %w", err)
	}
	fmt.Println("  Helper: done")
	return nil
}

func (c *UpgradeCmd) upgradeAgent(ctx context.Context, globals *CLI, targetVersion string) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		fmt.Printf("  Agent: skipped (no host configured)\n")
		return nil
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	// Warn if tunnel is running.
	if state, err := tunnel.LoadState(hostName); err == nil && state != nil {
		fmt.Fprintf(os.Stderr, "  Warning: tunnel is running (PID %d). The agent will restart.\n", state.PID)
	}

	if c.Local {
		paths := resolveLocalPaths("dist")
		os.Setenv("HOP_AGENT_BINARY", paths.agent)
	}

	// Pass empty targetVersion for --local so UpgradeAgent skips the
	// version comparison (dev builds always re-upload).
	agentVersion := targetVersion
	if c.Local {
		agentVersion = ""
	}

	fmt.Printf("  Agent (%s): upgrading...\n", hostName)
	return setup.UpgradeAgent(ctx, cfg, os.Stdout, agentVersion)
}

// atomicReplace writes data to path atomically via rename.
func atomicReplace(path string, data []byte) error {
	tmpPath := path + ".new"
	if err := os.WriteFile(tmpPath, data, 0755); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s → %s: %w", tmpPath, path, err)
	}
	return nil
}

// verifyChecksum downloads checksums.txt for the given release version and
// verifies the SHA256 of data matches the expected value for binName.
func verifyChecksum(ctx context.Context, releaseVersion, binName string, data []byte) error {
	csURL := fmt.Sprintf("%s/v%s/checksums.txt", releaseBaseURL, releaseVersion)
	expected, err := setup.LookupChecksum(ctx, csURL, binName)
	if err != nil {
		return fmt.Errorf("checksum lookup: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", binName, actual, expected)
	}
	return nil
}

// latestRelease queries the GitHub API for the latest release tag and returns
// the version string (without "v" prefix).
func latestRelease(ctx context.Context) (string, error) {
	data, err := setup.FetchURL(ctx, "https://api.github.com/repos/hopboxdev/hopbox/releases/latest")
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("parse release JSON: %w", err)
	}
	v := strings.TrimPrefix(release.TagName, "v")
	if v == "" {
		return "", fmt.Errorf("no tag_name in release response")
	}
	return v, nil
}
```

**Step 4: Update help text in main.go**

In `cmd/hop/main.go`, change:
```go
Upgrade   UpgradeCmd  `cmd:"" help:"Upgrade hop-agent binary on the remote host."`
```
to:
```go
Upgrade   UpgradeCmd  `cmd:"" help:"Upgrade hop binaries (client, helper, agent)."`
```

**Step 5: Run tests**

Run: `go test ./cmd/hop/... -run "TestAtomicReplace|TestResolveLocalPaths" -v`
Expected: PASS

Run: `go test ./...`
Expected: All PASS

**Step 6: Build and verify**

Run: `make build`
Expected: All three binaries build successfully

Run: `./dist/hop upgrade --help`
Expected: Shows new flags (`--version`, `--local`, `--client-only`, `--agent-only`, `--helper-only`)

**Step 7: Commit**

```bash
git add cmd/hop/upgrade.go cmd/hop/upgrade_test.go cmd/hop/main.go
git commit -m "feat: unified hop upgrade for client, helper, and agent"
```

---

### Manual Testing

After all tasks are complete, test the `--local` workflow:

```bash
# 1. Build all binaries
make build

# 2. Test --local upgrade (client + helper)
./dist/hop upgrade --local --agent-only=false --helper-only=false --client-only
# Should replace the running hop binary with the one from dist/

./dist/hop upgrade --local --helper-only
# Should prompt for sudo and reinstall the helper daemon

# 3. Test --local upgrade (agent, requires a configured host)
./dist/hop upgrade --local --agent-only
# Should upload dist/hop-agent-linux to the server and restart

# 4. Test version display
hop version
# Should show the new version
```
