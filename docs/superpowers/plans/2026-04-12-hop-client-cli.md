# hop Client CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a client-side CLI (`hop`) that wraps ssh/scp to give users ergonomic access to their Hopbox dev environments.

**Architecture:** Single Go binary at `cmd/hop/main.go` using kong for CLI parsing. Config loaded from `~/.config/hop/config.toml` with env var overrides. Commands shell out to the user's `ssh`/`scp` via `os/exec`.

**Tech Stack:** Go, kong (CLI), go-toml/v2 (config), os/exec (ssh/scp)

---

### File Structure

| File | Responsibility |
|---|---|
| `cmd/hop/main.go` | CLI entry point, kong struct, command dispatch |
| `cmd/hop/config.go` | Config loading: file + env + flag merge, config file path resolution |
| `cmd/hop/init.go` | Interactive `hop init` wizard |
| `cmd/hop/ssh.go` | `hop ssh` — build and exec SSH command |
| `cmd/hop/expose.go` | `hop expose` — build and exec SSH tunnel command |
| `cmd/hop/transfer.go` | `hop transfer` — build and exec SCP command |
| `cmd/hop/config_cmd.go` | `hop config` — print resolved config |
| `cmd/hop/config_test.go` | Tests for config loading and merge logic |
| `cmd/hop/transfer_test.go` | Tests for transfer path parsing |

---

### Task 0: Rename In-Container CLI from `hopbox` to `hop`

**Files:**
- Rename: `cmd/hopbox/` → `cmd/hop-box/` (Go package, avoids collision with `cmd/hop/`)
- Modify: `scripts/build-cli.sh` — update source path and output binary name
- Modify: `scripts/build-release.sh` — update CLI build path and output name
- Modify: `templates/Dockerfile.base` — update COPY and binary name
- Modify: `Makefile` — update clean target
- Modify: `cmd/hop-box/main.go` — change kong name from "hopbox" to "hop"

Note: The Go package directory is `cmd/hop-box/` to avoid colliding with the client `cmd/hop/`, but the compiled binary is named `hop` in both cases. They never coexist on the same machine (one is inside the container, one is on the user's laptop).

- [ ] **Step 1: Rename the directory**

```bash
git mv cmd/hopbox cmd/hop-box
```

- [ ] **Step 2: Update kong name in `cmd/hop-box/main.go`**

Change:
```go
ctx := kong.Parse(&cli, kong.Name("hopbox"), kong.Description("Hopbox dev environment CLI"))
```
To:
```go
ctx := kong.Parse(&cli, kong.Name("hop"), kong.Description("Hopbox dev environment CLI"))
```

- [ ] **Step 3: Update `scripts/build-cli.sh`**

Change:
```bash
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 \
    go build -o "$PROJECT_ROOT/templates/hopbox" "$PROJECT_ROOT/cmd/hopbox/"

echo "Built templates/hopbox for linux/$(go env GOARCH)"
```
To:
```bash
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 \
    go build -o "$PROJECT_ROOT/templates/hop" "$PROJECT_ROOT/cmd/hop-box/"

echo "Built templates/hop for linux/$(go env GOARCH)"
```

- [ ] **Step 4: Update `scripts/build-release.sh`**

Change:
```bash
# Build in-container hopbox CLI
echo "    building hopbox CLI..."
GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
  go build -o "${STAGE_DIR}/templates/hopbox" ./cmd/hopbox
```
To:
```bash
# Build in-container hop CLI
echo "    building hop CLI..."
GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 \
  go build -o "${STAGE_DIR}/templates/hop" ./cmd/hop-box
```

- [ ] **Step 5: Update `templates/Dockerfile.base`**

Change:
```dockerfile
# In-container hopbox CLI
COPY hopbox /usr/local/bin/hopbox
```
To:
```dockerfile
# In-container hop CLI
COPY hop /usr/local/bin/hop
```

- [ ] **Step 6: Update `Makefile` clean target**

Change:
```makefile
clean: ## Development: Remove build artifacts
	rm -f hopboxd hopbox templates/hopbox
	rm -rf dist/
```
To:
```makefile
clean: ## Development: Remove build artifacts
	rm -f hopboxd templates/hop
	rm -rf dist/
```

- [ ] **Step 7: Verify build**

Run: `go build ./cmd/hop-box/ && ./scripts/build-cli.sh`
Expected: both succeed, `templates/hop` is produced

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor: rename in-container CLI from hopbox to hop"
```

---

### Task 1: Config Loading

**Files:**
- Create: `cmd/hop/config.go`
- Create: `cmd/hop/config_test.go`

- [ ] **Step 1: Write failing tests for config loading**

```go
// cmd/hop/config_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
server = "hopbox.dev"
port = 2222
user = "gandalf"
default_box = "main"
`), 0644)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "hopbox.dev" {
		t.Errorf("server = %q, want %q", cfg.Server, "hopbox.dev")
	}
	if cfg.Port != 2222 {
		t.Errorf("port = %d, want %d", cfg.Port, 2222)
	}
	if cfg.User != "gandalf" {
		t.Errorf("user = %q, want %q", cfg.User, "gandalf")
	}
	if cfg.DefaultBox != "main" {
		t.Errorf("default_box = %q, want %q", cfg.DefaultBox, "main")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := loadConfig("/nonexistent/config.toml")
	if err != nil {
		t.Fatal("missing file should not error, just return defaults")
	}
	if cfg.Port != 2222 {
		t.Errorf("default port = %d, want 2222", cfg.Port)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
server = "hopbox.dev"
port = 2222
user = "gandalf"
default_box = "main"
`), 0644)

	t.Setenv("HOP_SERVER", "other.dev")
	t.Setenv("HOP_BOX", "work")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.applyEnv()

	if cfg.Server != "other.dev" {
		t.Errorf("server = %q, want %q", cfg.Server, "other.dev")
	}
	if cfg.DefaultBox != "work" {
		t.Errorf("default_box = %q, want %q", cfg.DefaultBox, "work")
	}
	if cfg.User != "gandalf" {
		t.Errorf("user should stay %q from file, got %q", "gandalf", cfg.User)
	}
}

func TestSSHUser(t *testing.T) {
	cfg := hopConfig{User: "gandalf", DefaultBox: "main"}
	if got := cfg.sshUser(); got != "gandalf+main" {
		t.Errorf("sshUser() = %q, want %q", got, "gandalf+main")
	}
}

func TestSSHUserNoBox(t *testing.T) {
	cfg := hopConfig{User: "gandalf"}
	if got := cfg.sshUser(); got != "gandalf" {
		t.Errorf("sshUser() = %q, want %q", got, "gandalf")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/hop/ -v -run TestLoadConfig`
Expected: FAIL — `loadConfig` undefined

- [ ] **Step 3: Implement config loading**

```go
// cmd/hop/config.go
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type hopConfig struct {
	Server     string `toml:"server"`
	Port       int    `toml:"port"`
	User       string `toml:"user"`
	DefaultBox string `toml:"default_box"`
}

func defaultConfig() hopConfig {
	return hopConfig{Port: 2222}
}

func loadConfig(path string) (hopConfig, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 2222
	}
	return cfg, nil
}

func (c *hopConfig) applyEnv() {
	if v := os.Getenv("HOP_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("HOP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := os.Getenv("HOP_USER"); v != "" {
		c.User = v
	}
	if v := os.Getenv("HOP_BOX"); v != "" {
		c.DefaultBox = v
	}
}

func (c *hopConfig) sshUser() string {
	if c.DefaultBox != "" {
		return c.User + "+" + c.DefaultBox
	}
	return c.User
}

func (c *hopConfig) sshUserWithBox(box string) string {
	if box != "" {
		return c.User + "+" + box
	}
	return c.sshUser()
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.config/hop/config.toml"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/hop/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/hop/config.go cmd/hop/config_test.go
git commit -m "feat(hop): add config loading with env var overrides"
```

---

### Task 2: CLI Entry Point and `hop init`

**Files:**
- Create: `cmd/hop/main.go`
- Create: `cmd/hop/init.go`

- [ ] **Step 1: Create CLI entry point with kong**

```go
// cmd/hop/main.go
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

type CLI struct {
	Init     InitCmd     `cmd:"" help:"Set up hop configuration."`
	SSH      SSHCmd      `cmd:"" help:"Open an SSH session to your box."`
	Expose   ExposeCmd   `cmd:"" help:"Forward a port from your box to localhost."`
	Transfer TransferCmd `cmd:"" help:"Upload a file to your box."`
	Config   ConfigCmd   `cmd:"" help:"Print resolved configuration."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox client CLI"),
		kong.UsageOnError(),
	)

	if ctx.Command() == "" {
		cfg, err := loadConfig(configPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cfg.applyEnv()
		if cfg.Server == "" || cfg.User == "" {
			ctx.PrintUsage(false)
			os.Exit(0)
		}
		sshCmd := SSHCmd{}
		if err := sshCmd.Run(&cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := ctx.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Implement `hop init`**

```go
// cmd/hop/init.go
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type InitCmd struct{}

func (c *InitCmd) Run() error {
	reader := bufio.NewReader(os.Stdin)

	existing, _ := loadConfig(configPath())

	fmt.Print("Server hostname: ")
	if existing.Server != "" {
		fmt.Printf("[%s]: ", existing.Server)
	}
	server := readLine(reader)
	if server == "" {
		server = existing.Server
	}

	fmt.Printf("SSH port [%d]: ", existing.Port)
	portStr := readLine(reader)
	port := existing.Port
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	fmt.Print("Username: ")
	if existing.User != "" {
		fmt.Printf("[%s]: ", existing.User)
	}
	user := readLine(reader)
	if user == "" {
		user = existing.User
	}

	fmt.Print("Default box: ")
	if existing.DefaultBox != "" {
		fmt.Printf("[%s]: ", existing.DefaultBox)
	}
	box := readLine(reader)
	if box == "" {
		box = existing.DefaultBox
	}

	cfg := hopConfig{
		Server:     server,
		Port:       port,
		User:       user,
		DefaultBox: box,
	}

	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\nConfig written to %s\n", path)
	return nil
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./cmd/hop/`
Expected: builds without errors

- [ ] **Step 4: Commit**

```bash
git add cmd/hop/main.go cmd/hop/init.go
git commit -m "feat(hop): add CLI entry point and init command"
```

---

### Task 3: `hop ssh`

**Files:**
- Create: `cmd/hop/ssh.go`

- [ ] **Step 1: Implement ssh command**

```go
// cmd/hop/ssh.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

type SSHCmd struct {
	Box string `help:"Box to connect to (overrides default)." short:"b"`
}

func (c *SSHCmd) Run(cfg ...*hopConfig) error {
	conf, err := resolveConfig(cfg)
	if err != nil {
		return err
	}

	sshUser := conf.sshUserWithBox(c.Box)

	args := []string{
		"-p", strconv.Itoa(conf.Port),
		sshUser + "@" + conf.Server,
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveConfig(passed []*hopConfig) (*hopConfig, error) {
	if len(passed) > 0 && passed[0] != nil {
		return passed[0], nil
	}
	cfg, err := loadConfig(configPath())
	if err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if cfg.Server == "" || cfg.User == "" {
		return nil, fmt.Errorf("not configured — run `hop init` first")
	}
	return &cfg, nil
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/hop/`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add cmd/hop/ssh.go
git commit -m "feat(hop): add ssh command"
```

---

### Task 4: `hop expose`

**Files:**
- Create: `cmd/hop/expose.go`

- [ ] **Step 1: Implement expose command**

```go
// cmd/hop/expose.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

type ExposeCmd struct {
	Port int    `arg:"" help:"Port to forward."`
	Box  string `help:"Box to tunnel to (overrides default)." short:"b"`
}

func (c *ExposeCmd) Run() error {
	cfg, err := resolveConfig(nil)
	if err != nil {
		return err
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	portStr := strconv.Itoa(c.Port)
	forward := fmt.Sprintf("%s:localhost:%s", portStr, portStr)

	args := []string{
		"-p", strconv.Itoa(cfg.Port),
		"-L", forward,
		"-N",
		sshUser + "@" + cfg.Server,
	}

	fmt.Printf("Forwarding localhost:%d -> box:%d (ctrl-c to stop)\n", c.Port, c.Port)

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	return cmd.Run()
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/hop/`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add cmd/hop/expose.go
git commit -m "feat(hop): add expose command for port forwarding"
```

---

### Task 5: `hop transfer`

**Files:**
- Create: `cmd/hop/transfer.go`
- Create: `cmd/hop/transfer_test.go`

- [ ] **Step 1: Write failing tests for path parsing**

```go
// cmd/hop/transfer_test.go
package main

import "testing"

func TestParseTransferTarget(t *testing.T) {
	tests := []struct {
		input      string
		wantLocal  string
		wantRemote string
	}{
		{"./file.txt", "./file.txt", "~/"},
		{"./file.txt:/home/dev/projects/", "./file.txt", "/home/dev/projects/"},
		{"/tmp/data.tar.gz:/opt/", "/tmp/data.tar.gz", "/opt/"},
		{"file.txt:", "file.txt", "~/"},
	}
	for _, tt := range tests {
		local, remote := parseTransferTarget(tt.input)
		if local != tt.wantLocal {
			t.Errorf("parseTransferTarget(%q) local = %q, want %q", tt.input, local, tt.wantLocal)
		}
		if remote != tt.wantRemote {
			t.Errorf("parseTransferTarget(%q) remote = %q, want %q", tt.input, remote, tt.wantRemote)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/hop/ -v -run TestParseTransfer`
Expected: FAIL — `parseTransferTarget` undefined

- [ ] **Step 3: Implement transfer command**

```go
// cmd/hop/transfer.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type TransferCmd struct {
	File string `arg:"" help:"Local file to upload. Append :/remote/path to set destination."`
	Box  string `help:"Box to upload to (overrides default)." short:"b"`
}

func parseTransferTarget(input string) (local, remote string) {
	i := strings.LastIndex(input, ":")
	if i < 0 {
		return input, "~/"
	}
	local = input[:i]
	remote = input[i+1:]
	if remote == "" {
		remote = "~/"
	}
	return local, remote
}

func (c *TransferCmd) Run() error {
	cfg, err := resolveConfig(nil)
	if err != nil {
		return err
	}

	local, remote := parseTransferTarget(c.File)

	if _, err := os.Stat(local); err != nil {
		return fmt.Errorf("file not found: %s", local)
	}

	sshUser := cfg.sshUserWithBox(c.Box)
	dest := fmt.Sprintf("%s@%s:%s", sshUser, cfg.Server, remote)

	args := []string{
		"-O",
		"-P", strconv.Itoa(cfg.Port),
		local,
		dest,
	}

	fmt.Printf("Uploading %s -> %s:%s\n", local, cfg.sshUserWithBox(c.Box), remote)

	cmd := exec.Command("scp", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/hop/ -v -run TestParseTransfer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/hop/transfer.go cmd/hop/transfer_test.go
git commit -m "feat(hop): add transfer command for file uploads"
```

---

### Task 6: `hop config`

**Files:**
- Create: `cmd/hop/config_cmd.go`

- [ ] **Step 1: Implement config command**

```go
// cmd/hop/config_cmd.go
package main

import (
	"fmt"
	"os"
	"strings"
)

type ConfigCmd struct{}

func (c *ConfigCmd) Run() error {
	path := configPath()
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}

	var overrides []string
	if os.Getenv("HOP_SERVER") != "" {
		overrides = append(overrides, "HOP_SERVER")
	}
	if os.Getenv("HOP_PORT") != "" {
		overrides = append(overrides, "HOP_PORT")
	}
	if os.Getenv("HOP_USER") != "" {
		overrides = append(overrides, "HOP_USER")
	}
	if os.Getenv("HOP_BOX") != "" {
		overrides = append(overrides, "HOP_BOX")
	}

	cfg.applyEnv()

	fmt.Printf("server:      %s\n", cfg.Server)
	fmt.Printf("port:        %d\n", cfg.Port)
	fmt.Printf("user:        %s\n", cfg.User)
	fmt.Printf("default_box: %s\n", cfg.DefaultBox)

	source := path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		source = "(no config file)"
	}
	if len(overrides) > 0 {
		fmt.Printf("source:      %s (overrides: %s)\n", source, strings.Join(overrides, ", "))
	} else {
		fmt.Printf("source:      %s\n", source)
	}

	return nil
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/hop/`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add cmd/hop/config_cmd.go
git commit -m "feat(hop): add config command to print resolved settings"
```

---

### Task 7: Wire kong `Run()` and Build Target

**Files:**
- Modify: `cmd/hop/main.go`
- Modify: `Makefile`

- [ ] **Step 1: Update main.go to use kong's Run binding**

Replace the `main()` function in `cmd/hop/main.go`:

```go
// cmd/hop/main.go
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

type CLI struct {
	Init     InitCmd     `cmd:"" help:"Set up hop configuration."`
	SSH      SSHCmd      `cmd:"" help:"Open an SSH session to your box."`
	Expose   ExposeCmd   `cmd:"" help:"Forward a port from your box to localhost."`
	Transfer TransferCmd `cmd:"" help:"Upload a file to your box."`
	Config   ConfigCmd   `cmd:"" help:"Print resolved configuration."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox client CLI"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	switch ctx.Command() {
	case "":
		cfg, err := loadConfig(configPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cfg.applyEnv()
		if cfg.Server == "" || cfg.User == "" {
			ctx.PrintUsage(false)
			os.Exit(0)
		}
		sshCmd := SSHCmd{}
		if err := sshCmd.Run(&cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		if err := ctx.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
```

- [ ] **Step 2: Add build target to Makefile**

Check the existing Makefile for the current build pattern, then add a `hop` target that mirrors it. Expected addition:

```makefile
build-hop:
	go build -o bin/hop ./cmd/hop
```

- [ ] **Step 3: Verify full build and tests**

Run: `go build ./cmd/hop/ && go test ./cmd/hop/ -v`
Expected: build succeeds, all tests pass

- [ ] **Step 4: Commit**

```bash
git add cmd/hop/main.go Makefile
git commit -m "feat(hop): wire kong commands and add build target"
```

---

### Task 8: Add hop to GitHub Actions Release Matrix

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Check the existing release workflow**

Read `.github/workflows/release.yml` to understand the current matrix build pattern for `hopboxd` and `hopbox`.

- [ ] **Step 2: Add `hop` to the build matrix**

Add `hop` as a third binary in the matrix alongside `hopboxd` and `hopbox`. It should produce `hop-linux-amd64` and `hop-linux-arm64` (and `hop-darwin-amd64`/`hop-darwin-arm64` since this is a client tool).

The exact change depends on the current workflow structure — mirror whatever pattern `hopbox` uses but add darwin targets since `hop` runs on developer machines, not servers.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add hop client binary to release matrix"
```
