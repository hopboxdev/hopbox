# Hopbox Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an SSH gateway where `ssh -p 2222 hop@localhost` authenticates by public key, auto-registers new users, and drops them into a Docker container running zellij with a persisted home directory.

**Architecture:** Single Go binary (`hopboxd`) using gliderlabs/ssh for the SSH server and Docker SDK for container management. File-based user store keyed by SSH key fingerprint. Containers run `sleep infinity` and we `docker exec` zellij into them on connect. Port forwarding rewrites `localhost` to the session's container IP.

**Tech Stack:** Go, gliderlabs/ssh, Docker SDK for Go, charmbracelet/huh, go-toml/v2

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/hopboxd/main.go` | Entry point: load config, init Docker client, init user store, ensure base image, start SSH server |
| `internal/config/config.go` | `Config` struct, TOML parsing, defaults, `--config` flag |
| `internal/gateway/username.go` | Parse `user+boxname` from SSH username string |
| `internal/users/store.go` | `Store` struct: load users from disk, lookup by fingerprint, save new user, check username uniqueness |
| `internal/users/register.go` | `RunRegistration()`: charmbracelet/huh form over SSH session, returns username |
| `internal/containers/image.go` | `EnsureBaseImage()`: hash templates, check Docker for existing tag, build if needed |
| `internal/containers/manager.go` | `Manager` struct: find/create/start container, exec with PTY, attach to SSH session |
| `internal/gateway/server.go` | `NewServer()`: wire up SSH server with auth callback, session handler, port forwarding |
| `internal/gateway/tunnel.go` | `direct-tcpip` handler: resolve container IP, dial, pipe |
| `templates/Dockerfile.base` | Ubuntu 24.04 base image with dev user and tool installs |
| `templates/stacks/tools.sh` | Install fzf, ripgrep, fd, bat, lazygit, direnv |
| `templates/stacks/runtimes.sh` | Install mise, Node LTS, Python 3.12 |

---

### Task 1: Project Scaffolding & Config

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/gandalfledev/Developer/hopbox
go mod init github.com/hopboxdev/hopbox
```

- [ ] **Step 2: Create .gitignore**

Create `.gitignore`:

```
data/
*.exe
```

- [ ] **Step 3: Write the failing test for config parsing**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 2222 {
		t.Errorf("expected port 2222, got %d", cfg.Port)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected data dir ./data, got %s", cfg.DataDir)
	}
	if cfg.HostKeyPath != "" {
		t.Errorf("expected empty host key path, got %s", cfg.HostKeyPath)
	}
	if !cfg.OpenRegistration {
		t.Error("expected open registration to be true by default")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`
port = 3333
data_dir = "/tmp/hopbox"
host_key_path = "/etc/hopbox/key"
open_registration = false
`), 0644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3333 {
		t.Errorf("expected port 3333, got %d", cfg.Port)
	}
	if cfg.DataDir != "/tmp/hopbox" {
		t.Errorf("expected data dir /tmp/hopbox, got %s", cfg.DataDir)
	}
	if cfg.HostKeyPath != "/etc/hopbox/key" {
		t.Errorf("expected host key path /etc/hopbox/key, got %s", cfg.HostKeyPath)
	}
	if cfg.OpenRegistration {
		t.Error("expected open registration to be false")
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/config.toml")
	if err != nil {
		t.Fatalf("missing file should return defaults, got error: %v", err)
	}
	if cfg.Port != 2222 {
		t.Errorf("expected default port, got %d", cfg.Port)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

```bash
go test ./internal/config/ -v
```

Expected: FAIL — `config` package doesn't exist yet.

- [ ] **Step 5: Install go-toml and implement config**

```bash
go get github.com/pelletier/go-toml/v2
```

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Port             int    `toml:"port"`
	DataDir          string `toml:"data_dir"`
	HostKeyPath      string `toml:"host_key_path"`
	OpenRegistration bool   `toml:"open_registration"`
}

func defaults() Config {
	return Config{
		Port:             2222,
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
	}
}

// Load reads config from path. If path is empty, tries ./config.toml.
// If the file doesn't exist, returns defaults.
func Load(path string) (Config, error) {
	cfg := defaults()

	if path == "" {
		path = "config.toml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/config/ -v
```

Expected: all 3 tests PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum .gitignore internal/config/
git commit -m "feat: add project scaffolding and config parsing"
```

---

### Task 2: Username Parsing

**Files:**
- Create: `internal/gateway/username.go`
- Create: `internal/gateway/username_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gateway/username_test.go`:

```go
package gateway

import "testing"

func TestParseUsername(t *testing.T) {
	tests := []struct {
		input   string
		user    string
		boxname string
	}{
		{"hop", "hop", "default"},
		{"hop+myproject", "hop", "myproject"},
		{"gandalf+dev", "gandalf", "dev"},
		{"user+my-box", "user", "my-box"},
		{"hop+", "hop", "default"},
		{"+box", "", "box"},
		{"simple", "simple", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			user, boxname := ParseUsername(tt.input)
			if user != tt.user {
				t.Errorf("user: got %q, want %q", user, tt.user)
			}
			if boxname != tt.boxname {
				t.Errorf("boxname: got %q, want %q", boxname, tt.boxname)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v
```

Expected: FAIL — `ParseUsername` not defined.

- [ ] **Step 3: Implement ParseUsername**

Create `internal/gateway/username.go`:

```go
package gateway

import "strings"

// ParseUsername splits an SSH username like "hop+boxname" into user and boxname.
// If no "+" separator or boxname is empty, boxname defaults to "default".
func ParseUsername(raw string) (user, boxname string) {
	parts := strings.SplitN(raw, "+", 2)
	user = parts[0]
	if len(parts) == 2 && parts[1] != "" {
		boxname = parts[1]
	} else {
		boxname = "default"
	}
	return user, boxname
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v
```

Expected: all 7 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/
git commit -m "feat: add SSH username parsing (user+boxname format)"
```

---

### Task 3: User Store

**Files:**
- Create: `internal/users/store.go`
- Create: `internal/users/store_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/users/store_test.go`:

```go
package users

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	fp := "SHA256_aa_bb_cc_dd"

	// Initially not found
	_, ok := store.LookupByFingerprint(fp)
	if ok {
		t.Fatal("expected user not found")
	}

	// Register
	u := User{
		Username:     "gandalf",
		KeyType:      "ed25519",
		RegisteredAt: time.Now().UTC().Truncate(time.Second),
	}
	err := store.Save(fp, u)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Lookup should work after reload
	store2 := NewStore(dir)
	got, ok := store2.LookupByFingerprint(fp)
	if !ok {
		t.Fatal("expected user found after reload")
	}
	if got.Username != "gandalf" {
		t.Errorf("username: got %q, want %q", got.Username, "gandalf")
	}
	if got.KeyType != "ed25519" {
		t.Errorf("key type: got %q, want %q", got.KeyType, "ed25519")
	}

	// Home dir should exist
	homeDir := filepath.Join(dir, fp, "home")
	if !dirExists(homeDir) {
		t.Errorf("expected home dir at %s", homeDir)
	}
}

func TestUsernameUniqueness(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	u := User{Username: "gandalf", KeyType: "ed25519", RegisteredAt: time.Now().UTC()}
	err := store.Save("SHA256_aa", u)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Same username, different fingerprint should fail
	err = store.Save("SHA256_bb", u)
	if err == nil {
		t.Fatal("expected error for duplicate username")
	}
}

func TestFingerprintFormat(t *testing.T) {
	fp := FormatFingerprint("SHA256:aa:bb:cc:dd")
	if fp != "SHA256_aa_bb_cc_dd" {
		t.Errorf("got %q, want %q", fp, "SHA256_aa_bb_cc_dd")
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
```

Add `"os"` to the import block (alongside `"path/filepath"`, `"testing"`, `"time"`).

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/users/ -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement user store**

Create `internal/users/store.go`:

```go
package users

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type User struct {
	Username     string    `toml:"username"`
	KeyType      string    `toml:"key_type"`
	RegisteredAt time.Time `toml:"registered_at"`
}

type Store struct {
	dir   string
	users map[string]User // fingerprint -> User
}

func NewStore(dir string) *Store {
	s := &Store{
		dir:   dir,
		users: make(map[string]User),
	}
	s.load()
	return s
}

func (s *Store) load() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fp := e.Name()
		path := filepath.Join(s.dir, fp, "user.toml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var u User
		if err := toml.Unmarshal(data, &u); err != nil {
			continue
		}
		s.users[fp] = u
	}
}

func (s *Store) LookupByFingerprint(fp string) (User, bool) {
	u, ok := s.users[fp]
	return u, ok
}

func (s *Store) Save(fp string, u User) error {
	// Check username uniqueness
	for existingFP, existing := range s.users {
		if existing.Username == u.Username && existingFP != fp {
			return fmt.Errorf("username %q already taken", u.Username)
		}
	}

	userDir := filepath.Join(s.dir, fp)
	homeDir := filepath.Join(userDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return fmt.Errorf("create user dirs: %w", err)
	}

	data, err := toml.Marshal(u)
	if err != nil {
		return fmt.Errorf("marshal user: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "user.toml"), data, 0644); err != nil {
		return fmt.Errorf("write user.toml: %w", err)
	}

	s.users[fp] = u
	return nil
}

// HomePath returns the path to the user's bind-mounted home directory.
func (s *Store) HomePath(fp string) string {
	return filepath.Join(s.dir, fp, "home")
}

// FormatFingerprint converts "SHA256:aa:bb:cc:dd" to "SHA256_aa_bb_cc_dd".
func FormatFingerprint(raw string) string {
	return strings.ReplaceAll(raw, ":", "_")
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/users/ -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/users/
git commit -m "feat: add file-based user store with fingerprint lookup"
```

---

### Task 4: Container Image Builder

**Files:**
- Create: `internal/containers/image.go`
- Create: `internal/containers/image_test.go`
- Create: `templates/Dockerfile.base`
- Create: `templates/stacks/tools.sh`
- Create: `templates/stacks/runtimes.sh`

- [ ] **Step 1: Write the Dockerfile and stack scripts**

Create `templates/Dockerfile.base`:

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    sudo curl wget git build-essential openssh-client \
    unzip xz-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create dev user
RUN useradd -m -s /bin/bash -u 1000 dev \
    && echo "dev ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers.d/dev

COPY stacks/tools.sh /tmp/stacks/tools.sh
COPY stacks/runtimes.sh /tmp/stacks/runtimes.sh

RUN bash /tmp/stacks/tools.sh
RUN bash /tmp/stacks/runtimes.sh

# Clean up
RUN rm -rf /tmp/stacks

USER dev
WORKDIR /home/dev

# Install mise runtimes as dev user
RUN mise install node@lts && mise use --global node@lts \
    && mise install python@3.12 && mise use --global python@3.12

CMD ["sleep", "infinity"]
```

Create `templates/stacks/tools.sh`:

```bash
#!/bin/bash
set -euo pipefail

# fzf
curl -fsSL https://github.com/junegunn/fzf/releases/latest/download/fzf-$(curl -s https://api.github.com/repos/junegunn/fzf/releases/latest | grep -o '"tag_name": "v[^"]*' | cut -d'v' -f2)-linux_amd64.tar.gz | tar -xz -C /usr/local/bin/

# ripgrep
apt-get update && apt-get install -y ripgrep

# fd
apt-get install -y fd-find
ln -sf /usr/bin/fdfind /usr/local/bin/fd

# bat
apt-get install -y bat
ln -sf /usr/bin/batcat /usr/local/bin/bat

# lazygit
LAZYGIT_VERSION=$(curl -s https://api.github.com/repos/jesseduffield/lazygit/releases/latest | grep -o '"tag_name": "v[^"]*' | cut -d'v' -f2)
curl -fsSL "https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" | tar -xz -C /usr/local/bin/ lazygit

# direnv
curl -sfL https://direnv.net/install.sh | bash

# zellij
curl -fsSL https://github.com/zellij-org/zellij/releases/latest/download/zellij-x86_64-unknown-linux-musl.tar.gz | tar -xz -C /usr/local/bin/

# neovim
curl -fsSL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz | tar -xz -C /opt/
ln -sf /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim

rm -rf /var/lib/apt/lists/*
```

Create `templates/stacks/runtimes.sh`:

```bash
#!/bin/bash
set -euo pipefail

# Install mise (runtime version manager)
curl https://mise.run | sh
mv /root/.local/bin/mise /usr/local/bin/mise

# Create mise config directory for dev user
mkdir -p /home/dev/.config/mise
chown -R dev:dev /home/dev/.config
```

- [ ] **Step 2: Write the failing test for image hashing**

Create `internal/containers/image_test.go`:

```go
package containers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashTemplates(t *testing.T) {
	dir := t.TempDir()

	// Create fake template files
	if err := os.MkdirAll(filepath.Join(dir, "stacks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.base"), []byte("FROM ubuntu:24.04"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stacks", "tools.sh"), []byte("apt install stuff"), 0644); err != nil {
		t.Fatal(err)
	}

	hash1, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 == "" {
		t.Fatal("expected non-empty hash")
	}

	// Same content = same hash
	hash2, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("expected stable hash, got %s and %s", hash1, hash2)
	}

	// Change content = different hash
	if err := os.WriteFile(filepath.Join(dir, "stacks", "tools.sh"), []byte("apt install different"), 0644); err != nil {
		t.Fatal(err)
	}
	hash3, err := HashTemplates(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash1 == hash3 {
		t.Error("expected different hash after content change")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/containers/ -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 4: Implement image builder**

Create `internal/containers/image.go`:

```go
package containers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
)

const baseImageRepo = "hopbox-base"

// HashTemplates computes a SHA256 hash of all files in the templates directory.
func HashTemplates(templatesDir string) (string, error) {
	h := sha256.New()
	var paths []string

	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(templatesDir, path)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(paths)

	for _, rel := range paths {
		fmt.Fprintf(h, "file:%s\n", rel)
		data, err := os.ReadFile(filepath.Join(templatesDir, rel))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// BaseImageTag returns the full image tag for the given hash.
func BaseImageTag(hash string) string {
	return fmt.Sprintf("%s:%s", baseImageRepo, hash)
}

// EnsureBaseImage checks if the base image exists for the given template hash.
// If not, it builds it from the templates directory.
func EnsureBaseImage(ctx context.Context, cli *client.Client, templatesDir string) (string, error) {
	hash, err := HashTemplates(templatesDir)
	if err != nil {
		return "", fmt.Errorf("hash templates: %w", err)
	}

	tag := BaseImageTag(hash)

	// Check if image already exists
	images, err := cli.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return "", fmt.Errorf("list images: %w", err)
	}
	for _, img := range images {
		for _, t := range img.RepoTags {
			if t == tag {
				return tag, nil
			}
		}
	}

	// Build the image
	buildCtx, err := archive.TarWithOptions(templatesDir, &archive.TarOptions{})
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}
	defer buildCtx.Close()

	resp, err := cli.ImageBuild(ctx, buildCtx, types.ImageBuildOptions{
		Dockerfile: "Dockerfile.base",
		Tags:       []string{tag},
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}
	defer resp.Body.Close()

	// Drain build output (required for build to complete)
	if _, err := io.Copy(os.Stderr, resp.Body); err != nil {
		return "", fmt.Errorf("read build output: %w", err)
	}

	return tag, nil
}
```

- [ ] **Step 5: Run tests to verify hash tests pass**

```bash
go get github.com/docker/docker/client
go get github.com/docker/docker/api/types
go get github.com/docker/docker/pkg/archive
go test ./internal/containers/ -v -run TestHash
```

Expected: `TestHashTemplates` PASS. (Docker-dependent tests will be integration tests later.)

- [ ] **Step 6: Commit**

```bash
git add internal/containers/ templates/
git commit -m "feat: add base image builder with template hashing"
```

---

### Task 5: Container Manager

**Files:**
- Create: `internal/containers/manager.go`
- Create: `internal/containers/manager_test.go`

- [ ] **Step 1: Write the failing test for container naming**

Create `internal/containers/manager_test.go`:

```go
package containers

import "testing"

func TestContainerName(t *testing.T) {
	tests := []struct {
		username string
		boxname  string
		want     string
	}{
		{"gandalf", "default", "hopbox-gandalf-default"},
		{"user", "myproject", "hopbox-user-myproject"},
	}
	for _, tt := range tests {
		got := ContainerName(tt.username, tt.boxname)
		if got != tt.want {
			t.Errorf("ContainerName(%q, %q) = %q, want %q", tt.username, tt.boxname, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/containers/ -v -run TestContainerName
```

Expected: FAIL — `ContainerName` not defined.

- [ ] **Step 3: Implement container manager**

Create `internal/containers/manager.go`:

```go
package containers

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// ContainerName returns the Docker container name for a user's devbox.
func ContainerName(username, boxname string) string {
	return fmt.Sprintf("hopbox-%s-%s", username, boxname)
}

type Manager struct {
	cli *client.Client
}

func NewManager(cli *client.Client) *Manager {
	return &Manager{cli: cli}
}

// EnsureRunning finds or creates a container and ensures it's running.
// Returns the container ID.
func (m *Manager) EnsureRunning(ctx context.Context, username, boxname, imageTag, homePath string) (string, error) {
	name := ContainerName(username, boxname)

	// Look for existing container
	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	if len(containers) > 0 {
		c := containers[0]
		if c.State != "running" {
			if err := m.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				return "", fmt.Errorf("start container: %w", err)
			}
		}
		return c.ID, nil
	}

	// Create new container
	resp, err := m.cli.ContainerCreate(ctx, &container.Config{
		Image:      imageTag,
		User:       "dev",
		WorkingDir: "/home/dev",
		Cmd:        []string{"sleep", "infinity"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: homePath,
				Target: "/home/dev",
			},
		},
	}, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

// Exec runs a command in the container with PTY and pipes I/O to the given
// reader/writer. Blocks until the exec process exits.
func (m *Manager) Exec(ctx context.Context, containerID string, cmd []string, stdin io.Reader, stdout io.Writer, resizeCh <-chan [2]uint) error {
	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	execResp, err := m.cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}

	attachResp, err := m.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return fmt.Errorf("exec attach: %w", err)
	}
	defer attachResp.Close()

	// Handle PTY resizes in background
	go func() {
		for size := range resizeCh {
			_ = m.cli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
				Height: uint(size[1]),
				Width:  uint(size[0]),
			})
		}
	}()

	// Pipe I/O
	errCh := make(chan error, 2)

	// stdin -> container
	go func() {
		_, err := io.Copy(attachResp.Conn, stdin)
		errCh <- err
	}()

	// container -> stdout
	go func() {
		_, err := io.Copy(stdout, attachResp.Reader)
		if err != nil {
			// Try stdcopy demux for non-TTY (shouldn't happen with Tty:true)
			_, err = stdcopy.StdCopy(stdout, stdout, attachResp.Reader)
		}
		errCh <- err
	}()

	// Wait for either direction to finish
	<-errCh
	return nil
}

// ContainerIP returns the IP address of a running container on the default bridge network.
func (m *Manager) ContainerIP(ctx context.Context, containerID string) (string, error) {
	info, err := m.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}
	ip := info.NetworkSettings.IPAddress
	if ip == "" {
		return "", fmt.Errorf("container %s has no IP address", containerID)
	}
	return ip, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/containers/ -v -run TestContainerName
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/containers/manager.go internal/containers/manager_test.go
git commit -m "feat: add container manager with lifecycle and exec"
```

---

### Task 6: User Registration TUI

**Files:**
- Create: `internal/users/register.go`
- Create: `internal/users/register_test.go`

- [ ] **Step 1: Write the failing test for username validation**

Create `internal/users/register_test.go`:

```go
package users

import "testing"

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"gandalf", true},
		{"my-user", true},
		{"user123", true},
		{"a", true},
		{"", false},
		{"has spaces", false},
		{"has@symbol", false},
		{"UPPER", false},
		{"-leading", false},
		{"trailing-", false},
		{"has--double", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateUsername(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid, got nil")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/users/ -v -run TestValidateUsername
```

Expected: FAIL — `ValidateUsername` not defined.

- [ ] **Step 3: Implement registration**

Create `internal/users/register.go`:

```go
package users

import (
	"fmt"
	"regexp"

	"io"

	"github.com/charmbracelet/huh"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateUsername checks that a username is lowercase alphanumeric with hyphens,
// not starting/ending with hyphen, no consecutive hyphens.
func ValidateUsername(name string) error {
	if name == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("username must be lowercase alphanumeric with single hyphens, not starting or ending with a hyphen")
	}
	if containsDoubleHyphen(name) {
		return fmt.Errorf("username cannot contain consecutive hyphens")
	}
	return nil
}

func containsDoubleHyphen(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '-' && s[i+1] == '-' {
			return true
		}
	}
	return false
}

// RunRegistration presents a TUI form over the SSH session to collect a username.
func RunRegistration(store *Store, in io.Reader, out io.Writer) (string, error) {
	var username string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Welcome to Hopbox!").
				Description("Choose a username for your dev environment.").
				Placeholder("username").
				Value(&username).
				Validate(func(s string) error {
					if err := ValidateUsername(s); err != nil {
						return err
					}
					// Check uniqueness
					for _, u := range store.users {
						if u.Username == s {
							return fmt.Errorf("username %q is already taken", s)
						}
					}
					return nil
				}),
		),
	).WithInput(in).WithOutput(out)

	if err := form.Run(); err != nil {
		return "", fmt.Errorf("registration form: %w", err)
	}

	return username, nil
}
```

- [ ] **Step 4: Install huh dependency and run tests**

```bash
go get github.com/charmbracelet/huh
go test ./internal/users/ -v -run TestValidateUsername
```

Expected: all 11 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/users/register.go internal/users/register_test.go
git commit -m "feat: add registration TUI with username validation"
```

---

### Task 7: SSH Server & Session Handler

**Files:**
- Create: `internal/gateway/server.go`

This task wires everything together. It's the core of hopboxd — the SSH server with auth, session handling, and the plumbing between SSH channels and Docker exec.

- [ ] **Step 1: Implement the SSH server**

Create `internal/gateway/server.go`:

```go
package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/users"
)

type Server struct {
	cfg      config.Config
	store    *users.Store
	manager  *containers.Manager
	imageTag string
	sshSrv   *ssh.Server
}

func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, imageTag string) (*Server, error) {
	s := &Server{
		cfg:      cfg,
		store:    store,
		manager:  manager,
		imageTag: imageTag,
	}

	hostKey, err := s.loadOrGenerateHostKey()
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	s.sshSrv = &ssh.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		PublicKeyHandler: s.authHandler,
		Handler:          s.sessionHandler,
		LocalPortForwardingCallback: func(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
			return true // allow all local port forwarding; tunnel handler rewrites destination
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": ssh.DirectTCPIPHandler,
		},
	}

	s.sshSrv.AddHostKey(hostKey)

	// Override the reverse-tcpip forwarding handler so tunnels route into containers
	s.sshSrv.RequestHandlers = map[string]ssh.RequestHandler{
		"tcpip-forward":        s.forwardHandler,
		"cancel-tcpip-forward": s.cancelForwardHandler,
	}

	return s, nil
}

func (s *Server) ListenAndServe() error {
	log.Printf("hopboxd listening on :%d", s.cfg.Port)
	return s.sshSrv.ListenAndServe()
}

// authHandler validates the SSH public key. Stores fingerprint and registration
// flag in the session context.
func (s *Server) authHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	fp := users.FormatFingerprint(gossh.FingerprintSHA256(key))
	ctx.SetValue("fingerprint", fp)
	ctx.SetValue("key_type", key.Type())

	_, known := s.store.LookupByFingerprint(fp)
	if known {
		ctx.SetValue("needs_registration", false)
		return true
	}

	if s.cfg.OpenRegistration {
		ctx.SetValue("needs_registration", true)
		return true
	}

	return false
}

// sessionHandler is called for each new SSH session after auth succeeds.
func (s *Server) sessionHandler(sess ssh.Session) {
	ctx := sess.Context()
	fp := ctx.Value("fingerprint").(string)
	needsReg := ctx.Value("needs_registration").(bool)

	_, boxname := ParseUsername(sess.User())

	// Registration flow for new users
	if needsReg {
		username, err := users.RunRegistration(s.store, sess, sess)
		if err != nil {
			fmt.Fprintf(sess, "Registration failed: %v\r\n", err)
			return
		}

		u := users.User{
			Username:     username,
			KeyType:      ctx.Value("key_type").(string),
			RegisteredAt: time.Now().UTC(),
		}
		if err := s.store.Save(fp, u); err != nil {
			fmt.Fprintf(sess, "Failed to save user: %v\r\n", err)
			return
		}

		fmt.Fprintf(sess, "Welcome, %s! Setting up your dev environment...\r\n", username)
	}

	user, ok := s.store.LookupByFingerprint(fp)
	if !ok {
		fmt.Fprintf(sess, "User not found\r\n")
		return
	}

	// Store container ID in context for tunnel handler
	homePath := s.store.HomePath(fp)
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, s.imageTag, homePath)
	if err != nil {
		fmt.Fprintf(sess, "Failed to start container: %v\r\n", err)
		return
	}
	ctx.SetValue("container_id", containerID)

	// Set up PTY resize channel
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		fmt.Fprintf(sess, "PTY required. Use: ssh -t ...\r\n")
		return
	}

	resizeCh := make(chan [2]uint, 1)
	// Send initial size
	resizeCh <- [2]uint{uint(ptyReq.Window.Width), uint(ptyReq.Window.Height)}

	go func() {
		for win := range winCh {
			resizeCh <- [2]uint{uint(win.Width), uint(win.Height)}
		}
		close(resizeCh)
	}()

	// Exec zellij in container
	cmd := []string{"zellij", "attach", "--create", "default"}
	if err := s.manager.Exec(ctx, containerID, cmd, sess, sess, resizeCh); err != nil {
		fmt.Fprintf(sess, "Session error: %v\r\n", err)
	}
}

// forwardHandler and cancelForwardHandler are no-ops for now;
// actual forwarding is handled by the direct-tcpip channel handler which
// gliderlabs/ssh routes through DirectTCPIPHandler.
func (s *Server) forwardHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	return true, nil
}

func (s *Server) cancelForwardHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	return true, nil
}

// loadOrGenerateHostKey loads the host key from disk or generates a new one.
func (s *Server) loadOrGenerateHostKey() (gossh.Signer, error) {
	keyPath := s.cfg.HostKeyPath

	if keyPath == "" {
		// Auto-generate mode
		keyPath = filepath.Join(s.cfg.DataDir, "host_key")

		if _, err := os.Stat(keyPath); err == nil {
			// Key already exists from previous run
			return loadHostKey(keyPath)
		}

		log.Printf("WARNING: No host key configured, auto-generating to %s", keyPath)
		if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
			return nil, err
		}
		return generateAndSaveHostKey(keyPath)
	}

	// Configured path — must exist
	return loadHostKey(keyPath)
}

func loadHostKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host key %s: %w", path, err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse host key: %w", err)
	}
	return signer, nil
}

func generateAndSaveHostKey(path string) (gossh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	privBytes, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return nil, err
	}

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	return signer, nil
}
```

- [ ] **Step 2: Install gliderlabs/ssh dependency**

```bash
go get github.com/gliderlabs/ssh
go get golang.org/x/crypto/ssh
```

- [ ] **Step 3: Verify the package compiles**

```bash
go build ./internal/gateway/
```

Expected: compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/server.go
git commit -m "feat: add SSH server with auth, session handler, and host key management"
```

---

### Task 8: Port Forwarding (Tunnel Handler)

**Files:**
- Create: `internal/gateway/tunnel.go`
- Create: `internal/gateway/tunnel_test.go`

- [ ] **Step 1: Write the failing test for destination rewriting**

Create `internal/gateway/tunnel_test.go`:

```go
package gateway

import "testing"

func TestRewriteDestination(t *testing.T) {
	tests := []struct {
		host        string
		containerIP string
		want        string
	}{
		{"localhost", "172.17.0.5", "172.17.0.5"},
		{"127.0.0.1", "172.17.0.5", "172.17.0.5"},
		{"::1", "172.17.0.5", "172.17.0.5"},
		{"0.0.0.0", "172.17.0.5", "172.17.0.5"},
		{"example.com", "172.17.0.5", "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := RewriteDestination(tt.host, tt.containerIP)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gateway/ -v -run TestRewriteDestination
```

Expected: FAIL — `RewriteDestination` not defined.

- [ ] **Step 3: Implement tunnel handler**

Create `internal/gateway/tunnel.go`:

```go
package gateway

// RewriteDestination maps localhost-like addresses to the container IP.
func RewriteDestination(host, containerIP string) string {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return containerIP
	default:
		return host
	}
}
```

Note: The actual `direct-tcpip` channel handling is done by `ssh.DirectTCPIPHandler` from gliderlabs/ssh, which we configured in `server.go`. The `LocalPortForwardingCallback` allows all forwarding requests. For Phase 1, this routes tunnels through the host's network — the host can reach container IPs on the Docker bridge. The `RewriteDestination` function is a utility for when we add a custom channel handler that intercepts and rewrites addresses. For now, the built-in handler works because the host can reach container IPs directly.

However, the built-in `DirectTCPIPHandler` dials the requested address from the host — meaning `localhost:3000` would hit the _host's_ port 3000, not the container's. We need a custom handler. Let's update `tunnel.go`:

```go
package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/containers"
)

// RewriteDestination maps localhost-like addresses to the container IP.
func RewriteDestination(host, containerIP string) string {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return containerIP
	default:
		return host
	}
}

// directTCPIPData mirrors the payload of a direct-tcpip channel open request.
type directTCPIPData struct {
	DestAddr string
	DestPort uint32
	SrcAddr  string
	SrcPort  uint32
}

// DirectTCPIPHandler returns a channel handler that rewrites localhost
// to the container IP for the session's user.
func DirectTCPIPHandler(mgr *containers.Manager) ssh.ChannelHandler {
	return func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshCtx ssh.Context) {
		var d directTCPIPData
		if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "failed to parse forward data")
			return
		}

		containerID, ok := sshCtx.Value("container_id").(string)
		if !ok {
			newChan.Reject(gossh.ConnectionFailed, "no container for session")
			return
		}

		containerIP, err := mgr.ContainerIP(context.Background(), containerID)
		if err != nil {
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("container IP: %v", err))
			return
		}

		dest := RewriteDestination(d.DestAddr, containerIP)
		addr := net.JoinHostPort(dest, fmt.Sprintf("%d", d.DestPort))

		var dialer net.Dialer
		conn2, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("dial %s: %v", addr, err))
			return
		}

		ch, reqs, err := newChan.Accept()
		if err != nil {
			conn2.Close()
			return
		}
		go gossh.DiscardRequests(reqs)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			io.Copy(ch, conn2)
			ch.CloseWrite()
		}()
		go func() {
			defer wg.Done()
			io.Copy(conn2, ch)
			conn2.Close()
		}()
		wg.Wait()
		ch.Close()
	}
}
```

- [ ] **Step 4: Update server.go to use custom DirectTCPIPHandler**

In `internal/gateway/server.go`, change the `ChannelHandlers` to:

```go
	s.sshSrv = &ssh.Server{
		Addr:             fmt.Sprintf(":%d", cfg.Port),
		PublicKeyHandler: s.authHandler,
		Handler:          s.sessionHandler,
		LocalPortForwardingCallback: func(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
			return true
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": DirectTCPIPHandler(manager),
		},
	}
```

And remove the old `RequestHandlers` block (the `forwardHandler` and `cancelForwardHandler` methods are no longer needed).

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/gateway/ -v
```

Expected: all tests PASS (username + tunnel rewrite).

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/tunnel.go internal/gateway/tunnel_test.go internal/gateway/server.go
git commit -m "feat: add port forwarding with container IP rewriting"
```

---

### Task 9: Entry Point (main.go)

**Files:**
- Create: `cmd/hopboxd/main.go`

- [ ] **Step 1: Implement main.go**

Create `cmd/hopboxd/main.go`:

```go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/gateway"
	"github.com/hopboxdev/hopbox/internal/users"
)

func main() {
	configPath := flag.String("config", "", "path to config.toml (default: ./config.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Ensure data directory exists
	usersDir := filepath.Join(cfg.DataDir, "users")
	if err := os.MkdirAll(usersDir, 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	// Verify Docker is reachable
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		log.Fatalf("cannot reach Docker daemon: %v", err)
	}

	// Ensure base image is built
	templatesDir := findTemplatesDir()
	imageTag, err := containers.EnsureBaseImage(ctx, cli, templatesDir)
	if err != nil {
		log.Fatalf("ensure base image: %v", err)
	}
	log.Printf("using base image: %s", imageTag)

	// Initialize user store
	store := users.NewStore(usersDir)

	// Initialize container manager
	mgr := containers.NewManager(cli)

	// Start SSH server
	srv, err := gateway.NewServer(cfg, store, mgr, imageTag)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// findTemplatesDir looks for the templates directory relative to the binary
// or the current working directory.
func findTemplatesDir() string {
	// Check relative to working directory first
	if info, err := os.Stat("templates"); err == nil && info.IsDir() {
		return "templates"
	}

	// Check relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "templates")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	log.Fatal("templates directory not found")
	return ""
}
```

- [ ] **Step 2: Verify the whole project compiles**

```bash
go mod tidy
go build ./cmd/hopboxd/
```

Expected: compiles to `hopboxd` binary without errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/hopboxd/main.go go.mod go.sum
git commit -m "feat: add hopboxd entry point wiring everything together"
```

---

### Task 10: Integration Smoke Test

This task verifies the whole system works end-to-end. Requires Docker running locally.

**Files:** None (manual testing)

- [ ] **Step 1: Build and run hopboxd**

```bash
go build -o hopboxd ./cmd/hopboxd/
./hopboxd
```

Expected output:
```
WARNING: No host key configured, auto-generating to ./data/host_key
using base image: hopbox-base:a1b2c3d4e5f6
hopboxd listening on :2222
```

(First run will take a few minutes to build the Docker image.)

- [ ] **Step 2: Test connection with SSH**

In a separate terminal:

```bash
ssh -p 2222 -o StrictHostKeyChecking=no hop@localhost
```

Expected: registration TUI prompt asking for a username. Enter a name, then land in a zellij session inside a container.

- [ ] **Step 3: Test reconnection**

Disconnect (Ctrl+D or `exit` from zellij), then reconnect:

```bash
ssh -p 2222 hop@localhost
```

Expected: reconnects to the existing zellij session with prior state preserved.

- [ ] **Step 4: Test boxname routing**

```bash
ssh -p 2222 hop+project1@localhost
```

Expected: creates a new container `hopbox-<username>-project1` with its own zellij session.

- [ ] **Step 5: Test port forwarding**

Inside the container, start a simple server:

```bash
python3 -m http.server 8080
```

In another terminal:

```bash
ssh -p 2222 -L 8080:localhost:8080 hop@localhost
```

Then verify from the host:

```bash
curl http://localhost:8080
```

Expected: serves the directory listing from inside the container, not the host.

- [ ] **Step 6: Verify user data persisted**

```bash
ls data/users/
cat data/users/SHA256_*/user.toml
```

Expected: user directory with `user.toml` and `home/` subdirectory.

- [ ] **Step 7: Commit any fixes from smoke testing**

```bash
git add -A
git commit -m "fix: address issues found during integration testing"
```

(Only if fixes were needed.)

---

## Dependency Summary

Install all dependencies at once if preferred:

```bash
go get github.com/gliderlabs/ssh
go get golang.org/x/crypto/ssh
go get github.com/docker/docker/client
go get github.com/docker/docker/api/types
go get github.com/docker/docker/pkg/archive
go get github.com/charmbracelet/huh
go get github.com/pelletier/go-toml/v2
```

## Task Dependency Graph

```
Task 1 (Config) ─────────────────────┐
Task 2 (Username Parsing) ───────────┤
Task 3 (User Store) ─────────────────┤
Task 4 (Image Builder) ──────────────┼──► Task 7 (SSH Server) ──► Task 9 (main.go) ──► Task 10 (Smoke Test)
Task 5 (Container Manager) ──────────┤           ▲
Task 6 (Registration TUI) ───────────┘           │
                                      Task 8 (Tunnel) ─┘
```

Tasks 1–6 are independent and can be built in parallel. Task 7 depends on all of them. Task 8 depends on 5 and 7. Task 9 depends on everything. Task 10 is the final verification.
