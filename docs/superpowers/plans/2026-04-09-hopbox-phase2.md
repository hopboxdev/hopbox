# Hopbox Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add interactive TUI wizard for tool selection, per-user Docker images layered on a shared base, and automatic container rebuilds when profiles change.

**Architecture:** Profile struct with TOML serialization drives a Dockerfile generator (builder). The wizard (charmbracelet/huh form) collects choices into a Profile. The session handler chains: registration → profile resolution → wizard (if needed) → image build → container lifecycle with label-based staleness detection. Base image slimmed to ubuntu + apt basics + mise. Per-user images layer tools on top.

**Tech Stack:** Go, charmbracelet/huh, Docker SDK for Go, go-toml/v2, runtime.GOARCH for arch detection

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/users/profile.go` | `Profile` struct, TOML read/write, hash, resolve (box vs user level), defaults |
| `internal/users/profile_test.go` | Tests for profile serialization, hashing, resolution |
| `internal/wizard/wizard.go` | `RunWizard()`: huh form for tool selection, takes defaults, returns `Profile` |
| `internal/containers/builder.go` | `GenerateDockerfile()` from Profile, `EnsureUserImage()` build + cache |
| `internal/containers/builder_test.go` | Tests for Dockerfile generation |
| `internal/containers/manager.go` | Modified: label tracking, profile hash mismatch → recreate, adaptive exec cmd |
| `internal/containers/image.go` | Modified: no functional changes, but base image templates change |
| `internal/gateway/server.go` | Modified: session handler chains wizard + profile + builder |
| `internal/gateway/tunnel.go` | Modified: `resolveContainerID` uses profile-aware flow |
| `cmd/hopboxd/main.go` | Modified: pass Docker client to builder |
| `templates/Dockerfile.base` | Slimmed: ubuntu + apt basics + mise only (no tools/runtimes) |

---

### Task 1: Profile Struct & Serialization

**Files:**
- Create: `internal/users/profile.go`
- Create: `internal/users/profile_test.go`

- [ ] **Step 1: Write the failing test for Profile defaults and TOML round-trip**

Create `internal/users/profile_test.go`:

```go
package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()
	if p.Multiplexer.Tool != "zellij" {
		t.Errorf("multiplexer: got %q, want %q", p.Multiplexer.Tool, "zellij")
	}
	if p.Editor.Tool != "neovim" {
		t.Errorf("editor: got %q, want %q", p.Editor.Tool, "neovim")
	}
	if p.Shell.Tool != "bash" {
		t.Errorf("shell: got %q, want %q", p.Shell.Tool, "bash")
	}
	if p.Runtimes.Node != "lts" {
		t.Errorf("node: got %q, want %q", p.Runtimes.Node, "lts")
	}
	if p.Runtimes.Python != "3.12" {
		t.Errorf("python: got %q, want %q", p.Runtimes.Python, "3.12")
	}
	if p.Runtimes.Go != "none" {
		t.Errorf("go: got %q, want %q", p.Runtimes.Go, "none")
	}
	if p.Runtimes.Rust != "none" {
		t.Errorf("rust: got %q, want %q", p.Runtimes.Rust, "none")
	}
	if len(p.Tools.Extras) != 5 {
		t.Errorf("extras: got %d tools, want 5", len(p.Tools.Extras))
	}
}

func TestProfileSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.toml")

	p := DefaultProfile()
	p.Editor.Tool = "vim"
	p.Runtimes.Go = "latest"

	if err := SaveProfile(path, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Editor.Tool != "vim" {
		t.Errorf("editor: got %q, want %q", loaded.Editor.Tool, "vim")
	}
	if loaded.Runtimes.Go != "latest" {
		t.Errorf("go: got %q, want %q", loaded.Runtimes.Go, "latest")
	}
}

func TestProfileHash(t *testing.T) {
	p1 := DefaultProfile()
	p2 := DefaultProfile()
	p3 := DefaultProfile()
	p3.Runtimes.Go = "latest"

	h1 := p1.Hash()
	h2 := p2.Hash()
	h3 := p3.Hash()

	if h1 != h2 {
		t.Errorf("same profiles should have same hash: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Error("different profiles should have different hashes")
	}
	if len(h1) != 12 {
		t.Errorf("hash should be 12 chars, got %d", len(h1))
	}
}

func TestResolveProfile(t *testing.T) {
	dir := t.TempDir()

	// No profile anywhere → nil
	p, err := ResolveProfile(dir, "default")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p != nil {
		t.Error("expected nil when no profile exists")
	}

	// Save user-level profile
	userProfile := DefaultProfile()
	userProfile.Editor.Tool = "vim"
	if err := SaveProfile(filepath.Join(dir, "profile.toml"), userProfile); err != nil {
		t.Fatalf("save user profile: %v", err)
	}

	// Resolve without box profile → user profile
	p, err = ResolveProfile(dir, "default")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p == nil {
		t.Fatal("expected user profile")
	}
	if p.Editor.Tool != "vim" {
		t.Errorf("editor: got %q, want %q", p.Editor.Tool, "vim")
	}

	// Save box-level profile → overrides user
	boxDir := filepath.Join(dir, "boxes", "mybox")
	if err := os.MkdirAll(boxDir, 0755); err != nil {
		t.Fatal(err)
	}
	boxProfile := DefaultProfile()
	boxProfile.Editor.Tool = "none"
	if err := SaveProfile(filepath.Join(boxDir, "profile.toml"), boxProfile); err != nil {
		t.Fatalf("save box profile: %v", err)
	}

	p, err = ResolveProfile(dir, "mybox")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Editor.Tool != "none" {
		t.Errorf("editor: got %q, want %q (box should override user)", p.Editor.Tool, "none")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/users/ -v -run "TestDefault|TestProfileSave|TestProfileHash|TestResolve"
```

Expected: FAIL — `DefaultProfile`, `SaveProfile`, `LoadProfile`, `ResolveProfile`, `Hash` not defined.

- [ ] **Step 3: Implement profile.go**

Create `internal/users/profile.go`:

```go
package users

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type MultiplexerConfig struct {
	Tool string `toml:"tool"` // "zellij" | "tmux"
}

type EditorConfig struct {
	Tool string `toml:"tool"` // "neovim" | "vim" | "none"
}

type ShellConfig struct {
	Tool string `toml:"tool"` // "bash" | "zsh" | "fish"
}

type RuntimesConfig struct {
	Node   string `toml:"node"`   // "lts" | "latest" | "none"
	Python string `toml:"python"` // "3.12" | "3.13" | "none"
	Go     string `toml:"go"`     // "latest" | "none"
	Rust   string `toml:"rust"`   // "latest" | "none"
}

type ToolsConfig struct {
	Extras []string `toml:"extras"`
}

type Profile struct {
	Multiplexer MultiplexerConfig `toml:"multiplexer"`
	Editor      EditorConfig      `toml:"editor"`
	Shell       ShellConfig       `toml:"shell"`
	Runtimes    RuntimesConfig    `toml:"runtimes"`
	Tools       ToolsConfig       `toml:"tools"`
}

func DefaultProfile() Profile {
	return Profile{
		Multiplexer: MultiplexerConfig{Tool: "zellij"},
		Editor:      EditorConfig{Tool: "neovim"},
		Shell:       ShellConfig{Tool: "bash"},
		Runtimes: RuntimesConfig{
			Node:   "lts",
			Python: "3.12",
			Go:     "none",
			Rust:   "none",
		},
		Tools: ToolsConfig{
			Extras: []string{"fzf", "ripgrep", "fd", "bat", "lazygit"},
		},
	}
}

func SaveProfile(path string, p Profile) error {
	data, err := toml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadProfile(path string) (Profile, error) {
	var p Profile
	data, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	return p, toml.Unmarshal(data, &p)
}

// Hash returns a 12-character hex hash of the profile for image tagging.
func (p Profile) Hash() string {
	data, _ := toml.Marshal(p)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:12]
}

// ResolveProfile loads the effective profile for a boxname within a user dir.
// Returns nil if no profile exists at either level.
func ResolveProfile(userDir, boxname string) (*Profile, error) {
	// Try box-level first
	boxPath := filepath.Join(userDir, "boxes", boxname, "profile.toml")
	if p, err := LoadProfile(boxPath); err == nil {
		return &p, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	// Fall back to user-level
	userPath := filepath.Join(userDir, "profile.toml")
	if p, err := LoadProfile(userPath); err == nil {
		return &p, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	return nil, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/users/ -v -run "TestDefault|TestProfileSave|TestProfileHash|TestResolve"
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/users/profile.go internal/users/profile_test.go
git commit -m "feat: add Profile struct with TOML serialization, hashing, and resolution"
```

---

### Task 2: Tool Selection Wizard

**Files:**
- Create: `internal/wizard/wizard.go`

- [ ] **Step 1: Implement the wizard**

Create `internal/wizard/wizard.go`:

```go
package wizard

import (
	"fmt"
	"io"

	"github.com/charmbracelet/huh"

	"github.com/hopboxdev/hopbox/internal/users"
)

// RunWizard presents the tool selection form over the SSH session.
// Takes a Profile as defaults (pre-filled). Returns the updated Profile.
func RunWizard(defaults users.Profile, in io.Reader, out io.Writer) (users.Profile, error) {
	p := defaults

	// Available CLI tools for multi-select
	toolOptions := []huh.Option[string]{
		huh.NewOption("fzf", "fzf"),
		huh.NewOption("ripgrep", "ripgrep"),
		huh.NewOption("fd", "fd"),
		huh.NewOption("bat", "bat"),
		huh.NewOption("lazygit", "lazygit"),
		huh.NewOption("direnv", "direnv"),
	}

	form := huh.NewForm(
		// Multiplexer
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Terminal Multiplexer").
				Options(
					huh.NewOption("zellij", "zellij"),
					huh.NewOption("tmux", "tmux"),
				).
				Value(&p.Multiplexer.Tool),
		),
		// Editor
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Editor").
				Options(
					huh.NewOption("neovim", "neovim"),
					huh.NewOption("vim", "vim"),
					huh.NewOption("none", "none"),
				).
				Value(&p.Editor.Tool),
		),
		// Shell
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Shell").
				Options(
					huh.NewOption("bash", "bash"),
					huh.NewOption("zsh", "zsh"),
					huh.NewOption("fish", "fish"),
				).
				Value(&p.Shell.Tool),
		),
		// Runtimes
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Node.js").
				Options(
					huh.NewOption("LTS", "lts"),
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).
				Value(&p.Runtimes.Node),
			huh.NewSelect[string]().
				Title("Python").
				Options(
					huh.NewOption("3.12", "3.12"),
					huh.NewOption("3.13", "3.13"),
					huh.NewOption("None", "none"),
				).
				Value(&p.Runtimes.Python),
			huh.NewSelect[string]().
				Title("Go").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).
				Value(&p.Runtimes.Go),
			huh.NewSelect[string]().
				Title("Rust").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).
				Value(&p.Runtimes.Rust),
		),
		// CLI Tools
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("CLI Tools").
				Options(toolOptions...).
				Value(&p.Tools.Extras),
		),
		// Confirm
		huh.NewGroup(
			huh.NewConfirm().
				Title("Build environment with these settings?").
				Affirmative("Confirm & Build").
				Negative("Cancel"),
		),
	).WithInput(in).WithOutput(out)

	if err := form.Run(); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	return p, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/wizard/
```

Expected: compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/wizard/wizard.go
git commit -m "feat: add tool selection wizard with huh form"
```

---

### Task 3: Slim Down Base Image

**Files:**
- Modify: `templates/Dockerfile.base`
- Remove: `templates/stacks/tools.sh`
- Remove: `templates/stacks/runtimes.sh`

- [ ] **Step 1: Replace Dockerfile.base with slimmed version**

Write `templates/Dockerfile.base`:

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    sudo curl wget git build-essential openssh-client \
    unzip xz-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create dev user (remove existing UID 1000 user if present, e.g. ubuntu)
RUN existing=$(getent passwd 1000 | cut -d: -f1) && \
    if [ -n "$existing" ]; then userdel -r "$existing"; fi && \
    useradd -m -s /bin/bash -u 1000 dev && \
    echo "dev ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers.d/dev

# Install mise (runtime version manager)
RUN curl https://mise.run | sh \
    && mv /root/.local/bin/mise /usr/local/bin/mise

# Set up mise data outside /home/dev (which gets bind-mounted)
ENV MISE_DATA_DIR=/opt/mise
ENV MISE_CONFIG_DIR=/opt/mise/config
RUN mkdir -p /opt/mise/config && chown -R dev:dev /opt/mise

# Activate mise in all bash sessions
RUN echo 'eval "$(/usr/local/bin/mise activate bash)"' >> /etc/bash.bashrc

USER dev
WORKDIR /home/dev

CMD ["sleep", "infinity"]
```

- [ ] **Step 2: Remove old stack scripts**

```bash
rm templates/stacks/tools.sh templates/stacks/runtimes.sh
rmdir templates/stacks
```

- [ ] **Step 3: Verify the project still builds**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add templates/Dockerfile.base
git rm templates/stacks/tools.sh templates/stacks/runtimes.sh
git commit -m "refactor: slim base image to ubuntu + apt basics + mise only"
```

---

### Task 4: Per-User Image Builder

**Files:**
- Create: `internal/containers/builder.go`
- Create: `internal/containers/builder_test.go`

- [ ] **Step 1: Write the failing test for Dockerfile generation**

Create `internal/containers/builder_test.go`:

```go
package containers

import (
	"strings"
	"testing"

	"github.com/hopboxdev/hopbox/internal/users"
)

func TestGenerateDockerfile(t *testing.T) {
	p := users.DefaultProfile()
	baseTag := "hopbox-base:abc123"

	df := GenerateDockerfile(p, baseTag)

	// Should start with FROM base
	if !strings.HasPrefix(df, "FROM hopbox-base:abc123\n") {
		t.Errorf("should start with FROM base, got: %s", df[:50])
	}

	// Should install zellij (default multiplexer)
	if !strings.Contains(df, "zellij") {
		t.Error("should install zellij")
	}

	// Should install neovim (default editor)
	if !strings.Contains(df, "nvim") {
		t.Error("should install neovim")
	}

	// Should NOT install zsh (default shell is bash, already in base)
	if strings.Contains(df, "apt-get install -y zsh") {
		t.Error("should not install zsh when shell is bash")
	}

	// Should install node and python runtimes
	if !strings.Contains(df, "mise install node@lts") {
		t.Error("should install node lts")
	}
	if !strings.Contains(df, "mise install python@3.12") {
		t.Error("should install python 3.12")
	}

	// Should NOT install go or rust (defaults are none)
	if strings.Contains(df, "mise install go") {
		t.Error("should not install go when set to none")
	}
	if strings.Contains(df, "mise install rust") {
		t.Error("should not install rust when set to none")
	}

	// Should install default CLI tools
	if !strings.Contains(df, "ripgrep") {
		t.Error("should install ripgrep")
	}
	if !strings.Contains(df, "fzf") {
		t.Error("should install fzf")
	}
}

func TestGenerateDockerfileMinimal(t *testing.T) {
	p := users.Profile{
		Multiplexer: users.MultiplexerConfig{Tool: "tmux"},
		Editor:      users.EditorConfig{Tool: "none"},
		Shell:       users.ShellConfig{Tool: "bash"},
		Runtimes: users.RuntimesConfig{
			Node: "none", Python: "none", Go: "none", Rust: "none",
		},
		Tools: users.ToolsConfig{Extras: []string{}},
	}
	baseTag := "hopbox-base:abc123"

	df := GenerateDockerfile(p, baseTag)

	// Should install tmux, not zellij
	if !strings.Contains(df, "tmux") {
		t.Error("should install tmux")
	}
	if strings.Contains(df, "zellij") {
		t.Error("should not install zellij")
	}

	// No editor
	if strings.Contains(df, "nvim") {
		t.Error("should not install neovim")
	}

	// No runtimes
	if strings.Contains(df, "mise install") {
		t.Error("should not install any runtimes")
	}

	// No CLI tools
	if strings.Contains(df, "ripgrep") {
		t.Error("should not install ripgrep")
	}
}

func TestUserImageTag(t *testing.T) {
	tag := UserImageTag("gandalf", "abc123def456")
	if tag != "hopbox-gandalf:abc123def456" {
		t.Errorf("got %q", tag)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/containers/ -v -run "TestGenerate|TestUserImage"
```

Expected: FAIL — `GenerateDockerfile`, `UserImageTag` not defined.

- [ ] **Step 3: Implement builder.go**

Create `internal/containers/builder.go`:

```go
package containers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/users"
)

// UserImageTag returns the Docker image tag for a user's profile.
func UserImageTag(username, profileHash string) string {
	return fmt.Sprintf("hopbox-%s:%s", username, profileHash)
}

// GenerateDockerfile produces a Dockerfile string from a Profile, layered on baseTag.
func GenerateDockerfile(p users.Profile, baseTag string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "FROM %s\n\nUSER root\n\n", baseTag)

	// Architecture detection for binary downloads
	arch := runtime.GOARCH // "amd64" or "arm64"
	linuxArch := "x86_64"
	if arch == "arm64" {
		linuxArch = "aarch64"
	}

	// Shell
	switch p.Shell.Tool {
	case "zsh":
		b.WriteString("RUN apt-get update && apt-get install -y zsh && rm -rf /var/lib/apt/lists/*\n")
		b.WriteString("RUN echo 'eval \"$(/usr/local/bin/mise activate zsh)\"' >> /etc/zsh/zshrc\n\n")
	case "fish":
		b.WriteString("RUN apt-get update && apt-get install -y fish && rm -rf /var/lib/apt/lists/*\n")
		b.WriteString("RUN mkdir -p /etc/fish/conf.d && echo '/usr/local/bin/mise activate fish | source' > /etc/fish/conf.d/mise.fish\n\n")
	}

	// Multiplexer
	switch p.Multiplexer.Tool {
	case "zellij":
		fmt.Fprintf(&b, "RUN curl -fsSL https://github.com/zellij-org/zellij/releases/latest/download/zellij-%s-unknown-linux-musl.tar.gz | tar -xz -C /usr/local/bin/\n\n", linuxArch)
	case "tmux":
		b.WriteString("RUN apt-get update && apt-get install -y tmux && rm -rf /var/lib/apt/lists/*\n\n")
	}

	// Editor
	switch p.Editor.Tool {
	case "neovim":
		archSuffix := "x86_64"
		if arch == "arm64" {
			archSuffix = "arm64"
		}
		fmt.Fprintf(&b, "RUN curl -fsSL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-%s.tar.gz | tar -xz -C /opt/ && ln -sf /opt/nvim-linux-%s/bin/nvim /usr/local/bin/nvim\n\n", archSuffix, archSuffix)
	case "vim":
		b.WriteString("RUN apt-get update && apt-get install -y vim && rm -rf /var/lib/apt/lists/*\n\n")
	}

	// CLI Tools
	aptTools := []string{}
	for _, tool := range p.Tools.Extras {
		switch tool {
		case "ripgrep":
			aptTools = append(aptTools, "ripgrep")
		case "fd":
			aptTools = append(aptTools, "fd-find")
		case "bat":
			aptTools = append(aptTools, "bat")
		}
	}
	if len(aptTools) > 0 {
		fmt.Fprintf(&b, "RUN apt-get update && apt-get install -y %s && rm -rf /var/lib/apt/lists/*\n", strings.Join(aptTools, " "))
		for _, tool := range p.Tools.Extras {
			if tool == "fd" {
				b.WriteString("RUN ln -sf /usr/bin/fdfind /usr/local/bin/fd\n")
			}
			if tool == "bat" {
				b.WriteString("RUN ln -sf /usr/bin/batcat /usr/local/bin/bat\n")
			}
		}
		b.WriteString("\n")
	}

	// Binary CLI tools (downloaded)
	for _, tool := range p.Tools.Extras {
		switch tool {
		case "fzf":
			fmt.Fprintf(&b, "RUN FZF_VERSION=$(curl -s https://api.github.com/repos/junegunn/fzf/releases/latest | grep -o '\"tag_name\": \"v[^\"]*' | cut -d'v' -f2) && curl -fsSL \"https://github.com/junegunn/fzf/releases/download/v${FZF_VERSION}/fzf-${FZF_VERSION}-linux_%s.tar.gz\" | tar -xz -C /usr/local/bin/\n", arch)
		case "lazygit":
			lgArch := "x86_64"
			if arch == "arm64" {
				lgArch = "arm64"
			}
			fmt.Fprintf(&b, "RUN LAZYGIT_VERSION=$(curl -s https://api.github.com/repos/jesseduffield/lazygit/releases/latest | grep -o '\"tag_name\": \"v[^\"]*' | cut -d'v' -f2) && curl -fsSL \"https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_%s.tar.gz\" | tar -xz -C /usr/local/bin/ lazygit\n", lgArch)
		case "direnv":
			b.WriteString("RUN curl -sfL https://direnv.net/install.sh | bash\n")
		}
	}
	b.WriteString("\n")

	// Switch to dev user for runtime installs
	b.WriteString("USER dev\nWORKDIR /home/dev\n\n")

	// Runtimes via mise
	if p.Runtimes.Node != "none" {
		fmt.Fprintf(&b, "RUN mise install node@%s && mise use --global node@%s\n", p.Runtimes.Node, p.Runtimes.Node)
	}
	if p.Runtimes.Python != "none" {
		fmt.Fprintf(&b, "RUN mise install python@%s && mise use --global python@%s\n", p.Runtimes.Python, p.Runtimes.Python)
	}
	if p.Runtimes.Go != "none" {
		fmt.Fprintf(&b, "RUN mise install go@%s && mise use --global go@%s\n", p.Runtimes.Go, p.Runtimes.Go)
	}
	if p.Runtimes.Rust != "none" {
		fmt.Fprintf(&b, "RUN mise install rust@%s && mise use --global rust@%s\n", p.Runtimes.Rust, p.Runtimes.Rust)
	}

	b.WriteString("\nCMD [\"sleep\", \"infinity\"]\n")

	return b.String()
}

// EnsureUserImage checks if the per-user image exists. If not, builds it.
func EnsureUserImage(ctx context.Context, cli *client.Client, username string, p users.Profile, baseTag string) (string, error) {
	hash := p.Hash()
	tag := UserImageTag(username, hash)

	// Check if image already exists
	images, err := cli.ImageList(ctx, image.ListOptions{})
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

	log.Printf("[builder] building image %s for user %s", tag, username)

	dockerfile := GenerateDockerfile(p, baseTag)

	// Create a tar archive with just the Dockerfile
	tarBuf, err := dockerfileTar(dockerfile)
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}

	resp, err := cli.ImageBuild(ctx, tarBuf, build.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{tag},
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}
	defer resp.Body.Close()

	// Parse build output for errors
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Stream != "" {
			fmt.Fprint(os.Stderr, msg.Stream)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("build failed: %s", msg.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read build output: %w", err)
	}

	return tag, nil
}
```

Wait — the `dockerfileTar` function needs the archive package. Let me use the same approach as `image.go`. Actually, let me use a temp directory and the moby/go-archive package that's already a dependency.

Replace the `dockerfileTar` function and `EnsureUserImage` to use the archive approach properly:

```go
// EnsureUserImage checks if the per-user image exists. If not, builds it.
func EnsureUserImage(ctx context.Context, cli *client.Client, username string, p users.Profile, baseTag string) (string, error) {
	hash := p.Hash()
	tag := UserImageTag(username, hash)

	// Check if image already exists
	images, err := cli.ImageList(ctx, image.ListOptions{})
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

	log.Printf("[builder] building image %s for user %s", tag, username)

	dockerfile := GenerateDockerfile(p, baseTag)

	// Write Dockerfile to temp dir for build context
	tmpDir, err := os.MkdirTemp("", "hopbox-build-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return "", fmt.Errorf("write dockerfile: %w", err)
	}

	buildCtx, err := archive.TarWithOptions(tmpDir, &archive.TarOptions{})
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}
	defer buildCtx.Close()

	resp, err := cli.ImageBuild(ctx, buildCtx, build.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{tag},
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}
	defer resp.Body.Close()

	// Parse build output for errors
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Stream != "" {
			fmt.Fprint(os.Stderr, msg.Stream)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("build failed: %s", msg.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read build output: %w", err)
	}

	return tag, nil
}
```

The full `builder.go` should have these imports:

```go
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/moby/go-archive"

	"github.com/hopboxdev/hopbox/internal/users"
)
```

Note: use `github.com/moby/go-archive` (same as in `image.go`), not `github.com/docker/docker/pkg/archive`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/containers/ -v -run "TestGenerate|TestUserImage"
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/containers/builder.go internal/containers/builder_test.go
git commit -m "feat: add per-user image builder with Dockerfile generation"
```

---

### Task 5: Container Manager — Label Tracking & Profile-Aware Lifecycle

**Files:**
- Modify: `internal/containers/manager.go`

- [ ] **Step 1: Write the failing test for label matching**

Add to `internal/containers/manager_test.go`:

```go
func TestProfileHashLabel(t *testing.T) {
	label := ProfileHashLabel("abc123def456")
	if label != "hopbox.profile-hash" {
		t.Errorf("got key %q", label)
	}
}
```

Actually, the label key is a constant. Let me test the more important logic — `ShouldRecreate`:

Add to `internal/containers/manager_test.go`:

```go
func TestShouldRecreate(t *testing.T) {
	tests := []struct {
		name           string
		containerImage string
		containerLabel string
		wantImage      string
		wantHash       string
		want           bool
	}{
		{"matching", "hopbox-gandalf:abc123", "abc123", "hopbox-gandalf:abc123", "abc123", false},
		{"different hash", "hopbox-gandalf:abc123", "abc123", "hopbox-gandalf:def456", "def456", true},
		{"no label", "hopbox-gandalf:abc123", "", "hopbox-gandalf:def456", "def456", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRecreate(tt.containerLabel, tt.wantHash)
			if got != tt.want {
				t.Errorf("ShouldRecreate(%q, %q) = %v, want %v", tt.containerLabel, tt.wantHash, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/containers/ -v -run TestShouldRecreate
```

- [ ] **Step 3: Update manager.go**

Modify `internal/containers/manager.go`:

Add constant and helper:

```go
const profileHashLabelKey = "hopbox.profile-hash"

// ShouldRecreate returns true if the container's profile hash label doesn't match the desired hash.
func ShouldRecreate(containerLabel, wantHash string) bool {
	return containerLabel != wantHash
}
```

Change `EnsureRunning` signature to accept profile hash and image tag (instead of a single imageTag):

```go
func (m *Manager) EnsureRunning(ctx context.Context, username, boxname, imageTag, profileHash, homePath string) (string, error) {
	name := ContainerName(username, boxname)

	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	if len(containers) > 0 {
		c := containers[0]
		existingHash := c.Labels[profileHashLabelKey]

		if ShouldRecreate(existingHash, profileHash) {
			log.Printf("[container] profile changed for %s, recreating (old=%s new=%s)", name, existingHash, profileHash)
			if err := m.cli.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
				log.Printf("[container] stop old container: %v", err)
			}
			if err := m.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				return "", fmt.Errorf("remove old container: %w", err)
			}
		} else {
			if c.State != "running" {
				if err := m.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
					return "", fmt.Errorf("start container: %w", err)
				}
			}
			return c.ID, nil
		}
	}

	cfg := &container.Config{
		Image:      imageTag,
		User:       "dev",
		WorkingDir: "/home/dev",
		Cmd:        []string{"sleep", "infinity"},
		Labels: map[string]string{
			profileHashLabelKey: profileHash,
		},
	}
	hostCfg := &container.HostConfig{
		Binds: []string{fmt.Sprintf("%s:/home/dev", homePath)},
	}

	log.Printf("[container] creating %s with bind mount %s -> /home/dev", name, homePath)

	resp, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/containers/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/containers/manager.go internal/containers/manager_test.go
git commit -m "feat: add profile hash label tracking and container recreation on mismatch"
```

---

### Task 6: Update Session Handler

**Files:**
- Modify: `internal/gateway/server.go`

This task wires the wizard, profile, and builder into the session handler.

- [ ] **Step 1: Update imports and Server struct**

In `internal/gateway/server.go`, add imports:

```go
import (
	// ... existing imports ...
	"github.com/hopboxdev/hopbox/internal/wizard"
	"github.com/docker/docker/client"
)
```

Change `Server` struct to hold the Docker client and base image tag:

```go
type Server struct {
	cfg        config.Config
	store      *users.Store
	manager    *containers.Manager
	dockerCli  *client.Client
	baseTag    string
	sshSrv     *ssh.Server
}
```

Update `NewServer` signature:

```go
func NewServer(cfg config.Config, store *users.Store, manager *containers.Manager, dockerCli *client.Client, baseTag string) (*Server, error) {
```

And the tunnel handler:

```go
"direct-tcpip": DirectTCPIPHandler(s.manager, s.store, s.dockerCli, s.baseTag),
```

- [ ] **Step 2: Rewrite sessionHandler to chain wizard + profile + builder**

Replace the session handler body (after registration block) with:

```go
	user, ok := s.store.LookupByFingerprint(fp)
	if !ok {
		log.Printf("[session] user not found for fp=%s", fp[:20])
		fmt.Fprintf(sess, "User not found\r\n")
		return
	}

	log.Printf("[session] connect user=%s box=%s", user.Username, boxname)

	// Resolve profile
	userDir := filepath.Join(s.store.Dir(), fp)
	profile, err := users.ResolveProfile(userDir, boxname)
	if err != nil {
		log.Printf("[session] resolve profile failed: %v", err)
		fmt.Fprintf(sess, "Failed to load profile: %v\r\n", err)
		return
	}

	// Run wizard if no profile exists
	if profile == nil {
		defaults := users.DefaultProfile()
		// Try loading user-level defaults for new boxes
		if userDefault, err := users.ResolveProfile(userDir, "__nonexistent__"); err == nil && userDefault != nil {
			defaults = *userDefault
		}

		chosen, err := wizard.RunWizard(defaults, sess, sess)
		if err != nil {
			log.Printf("[session] wizard failed: %v", err)
			fmt.Fprintf(sess, "Setup cancelled.\r\n")
			// Use defaults on cancel
			chosen = defaults
		}

		// Save profile
		if needsReg {
			// First-time user: save as user-level default
			if err := users.SaveProfile(filepath.Join(userDir, "profile.toml"), chosen); err != nil {
				log.Printf("[session] save user profile failed: %v", err)
			}
		}
		// Always save box-level profile
		boxDir := filepath.Join(userDir, "boxes", boxname)
		os.MkdirAll(boxDir, 0755)
		if err := users.SaveProfile(filepath.Join(boxDir, "profile.toml"), chosen); err != nil {
			log.Printf("[session] save box profile failed: %v", err)
		}
		profile = &chosen
	}

	// Ensure per-user image
	imageTag, err := containers.EnsureUserImage(ctx, s.dockerCli, user.Username, *profile, s.baseTag)
	if err != nil {
		log.Printf("[session] build image failed: %v", err)
		fmt.Fprintf(sess, "Failed to build environment: %v\r\n", err)
		return
	}

	// Container lifecycle
	homePath := s.store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		log.Printf("[session] create home dir failed: %v", err)
		fmt.Fprintf(sess, "Failed to create home directory: %v\r\n", err)
		return
	}

	profileHash := profile.Hash()
	containerID, err := s.manager.EnsureRunning(ctx, user.Username, boxname, imageTag, profileHash, homePath)
	if err != nil {
		log.Printf("[session] container failed user=%s box=%s: %v", user.Username, boxname, err)
		fmt.Fprintf(sess, "Failed to start container: %v\r\n", err)
		return
	}
	ctx.SetValue("container_id", containerID)

	log.Printf("[session] attached user=%s box=%s container=%s", user.Username, boxname, containerID[:12])

	// PTY setup (existing code)
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		log.Printf("[session] no PTY user=%s", user.Username)
		fmt.Fprintf(sess, "PTY required. Use: ssh -t ...\r\n")
		return
	}

	resizeCh := make(chan [2]uint, 1)
	resizeCh <- [2]uint{uint(ptyReq.Window.Width), uint(ptyReq.Window.Height)}

	go func() {
		for win := range winCh {
			resizeCh <- [2]uint{uint(win.Width), uint(win.Height)}
		}
		close(resizeCh)
	}()

	// Adaptive exec based on profile
	term := ptyReq.Term
	if term == "" {
		term = "xterm-256color"
	}

	shellBin := "/bin/bash"
	switch profile.Shell.Tool {
	case "zsh":
		shellBin = "/usr/bin/zsh"
	case "fish":
		shellBin = "/usr/bin/fish"
	}

	var muxCmd string
	switch profile.Multiplexer.Tool {
	case "zellij":
		muxCmd = "zellij attach --create default"
	case "tmux":
		muxCmd = "tmux new-session -As default"
	}

	shellCmd := fmt.Sprintf(
		`if ! infocmp %s >/dev/null 2>&1; then export TERM=xterm-256color; else export TERM=%s; fi; export SHELL=%s; exec %s`,
		term, term, shellBin, muxCmd,
	)
	cmd := []string{"bash", "-c", shellCmd}
	env := []string{fmt.Sprintf("TERM=%s", term), fmt.Sprintf("SHELL=%s", shellBin)}

	if err := s.manager.Exec(ctx, containerID, cmd, env, sess, sess, resizeCh); err != nil {
		log.Printf("[session] exec error user=%s: %v", user.Username, err)
		fmt.Fprintf(sess, "Session error: %v\r\n", err)
	}

	log.Printf("[session] disconnect user=%s box=%s", user.Username, boxname)
	sess.Exit(0)
```

- [ ] **Step 3: Add Dir() method to Store**

In `internal/users/store.go`, add:

```go
// Dir returns the base directory of the store.
func (s *Store) Dir() string {
	return s.dir
}
```

- [ ] **Step 4: Update tunnel.go resolveContainerID**

The `resolveContainerID` function in `tunnel.go` needs to be updated to use the profile-aware flow. Change its signature and body:

```go
func resolveContainerID(sshCtx ssh.Context, mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string) (string, error) {
	if id, ok := sshCtx.Value("container_id").(string); ok && id != "" {
		return id, nil
	}

	fp, ok := sshCtx.Value("fingerprint").(string)
	if !ok {
		return "", fmt.Errorf("no fingerprint in session")
	}

	user, ok := store.LookupByFingerprint(fp)
	if !ok {
		return "", fmt.Errorf("unknown user")
	}

	_, boxname := ParseUsername(sshCtx.User())
	userDir := filepath.Join(store.Dir(), fp)

	profile, err := users.ResolveProfile(userDir, boxname)
	if err != nil {
		return "", fmt.Errorf("resolve profile: %w", err)
	}
	if profile == nil {
		p := users.DefaultProfile()
		profile = &p
	}

	imageTag, err := containers.EnsureUserImage(context.Background(), dockerCli, user.Username, *profile, baseTag)
	if err != nil {
		return "", fmt.Errorf("ensure image: %w", err)
	}

	homePath := store.HomePath(fp, boxname)
	if err := os.MkdirAll(homePath, 0755); err != nil {
		return "", fmt.Errorf("create home dir: %w", err)
	}

	profileHash := profile.Hash()
	containerID, err := mgr.EnsureRunning(context.Background(), user.Username, boxname, imageTag, profileHash, homePath)
	if err != nil {
		return "", err
	}

	sshCtx.SetValue("container_id", containerID)
	return containerID, nil
}
```

Update `DirectTCPIPHandler` signature:

```go
func DirectTCPIPHandler(mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string) ssh.ChannelHandler {
```

And the call inside:

```go
containerID, err := resolveContainerID(sshCtx, mgr, store, dockerCli, baseTag)
```

Add imports to tunnel.go:

```go
"path/filepath"
"github.com/docker/docker/client"
```

- [ ] **Step 5: Update main.go**

In `cmd/hopboxd/main.go`, change the `NewServer` call:

```go
srv, err := gateway.NewServer(cfg, store, mgr, cli, imageTag)
```

(Add `cli` as the Docker client parameter.)

- [ ] **Step 6: Verify the whole project compiles**

```bash
go build ./...
```

Expected: compiles without errors.

- [ ] **Step 7: Run all tests**

```bash
go test ./... -v
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/gateway/server.go internal/gateway/tunnel.go internal/users/store.go cmd/hopboxd/main.go
git commit -m "feat: wire wizard, profile, and builder into session handler"
```

---

### Task 7: Integration Smoke Test

**Files:** None (manual testing)

- [ ] **Step 1: Clean up old data and containers**

```bash
/usr/local/bin/docker rm -f $(/usr/local/bin/docker ps -aq --filter "name=hopbox-") 2>/dev/null
rm -rf data/users/
```

- [ ] **Step 2: Build and run**

```bash
go build -o hopboxd ./cmd/hopboxd/ && ./hopboxd
```

First run will rebuild the slimmed base image (since templates changed).

- [ ] **Step 3: Test first connection — should show wizard**

```bash
ssh -p 2222 hop@localhost
```

Expected: registration prompt (username), then wizard form with tool selection, then image build, then land in chosen multiplexer.

- [ ] **Step 4: Test reconnection — should skip wizard**

Disconnect, reconnect:

```bash
ssh -p 2222 hop@localhost
```

Expected: goes straight to container, no wizard.

- [ ] **Step 5: Test new boxname — should show wizard with defaults**

```bash
ssh -p 2222 hop+project1@localhost
```

Expected: wizard pre-filled with user's default choices. Can modify and save.

- [ ] **Step 6: Verify profile files saved**

```bash
cat data/users/SHA256_*/profile.toml
cat data/users/SHA256_*/boxes/default/profile.toml
cat data/users/SHA256_*/boxes/project1/profile.toml
```

- [ ] **Step 7: Commit any fixes**

```bash
git add -A
git commit -m "fix: address issues found during Phase 2 integration testing"
```

---

## Task Dependency Graph

```
Task 1 (Profile) ──────────────┐
Task 2 (Wizard) ────────────────┤
Task 3 (Slim Base Image) ───────┼──► Task 6 (Wire Session Handler) ──► Task 7 (Smoke Test)
Task 4 (Builder) ───────────────┤
Task 5 (Manager Labels) ────────┘
```

Tasks 1-5 are independent and can be built in parallel (though Task 4 depends on Task 1's Profile type). Task 6 wires everything together. Task 7 is the final verification.
