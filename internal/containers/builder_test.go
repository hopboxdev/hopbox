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

	if !strings.HasPrefix(df, "FROM hopbox-base:abc123\n") {
		t.Errorf("should start with FROM base, got: %s", df[:50])
	}
	if !strings.Contains(df, "zellij") {
		t.Error("should install zellij")
	}
	if !strings.Contains(df, "nvim") {
		t.Error("should install neovim")
	}
	if strings.Contains(df, "apt-get install -y zsh") {
		t.Error("should not install zsh when shell is bash")
	}
	// Default profile has all runtimes set to "none"
	if strings.Contains(df, "mise install") {
		t.Error("should not install any runtimes when all set to none")
	}
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

	if !strings.Contains(df, "tmux") {
		t.Error("should install tmux")
	}
	if strings.Contains(df, "zellij") {
		t.Error("should not install zellij")
	}
	if strings.Contains(df, "nvim") {
		t.Error("should not install neovim")
	}
	if strings.Contains(df, "mise install") {
		t.Error("should not install any runtimes")
	}
	if strings.Contains(df, "ripgrep") {
		t.Error("should not install ripgrep")
	}
}

func TestUserImageTag(t *testing.T) {
	tag := UserImageTag("gandalf", "abc123def456", "hopbox-base:xyz789")
	if !strings.HasPrefix(tag, "hopbox-gandalf:") {
		t.Errorf("got %q, want prefix hopbox-gandalf:", tag)
	}
	// Same inputs produce same tag
	tag2 := UserImageTag("gandalf", "abc123def456", "hopbox-base:xyz789")
	if tag != tag2 {
		t.Errorf("not deterministic: %q != %q", tag, tag2)
	}
	// Different base tag produces different user tag
	tag3 := UserImageTag("gandalf", "abc123def456", "hopbox-base:different")
	if tag == tag3 {
		t.Error("different base tag should produce different user tag")
	}
}

func TestGenerateDockerfileGH(t *testing.T) {
	p := users.Profile{
		Multiplexer: users.MultiplexerConfig{Tool: "none"},
		Editor:      users.EditorConfig{Tool: "none"},
		Shell:       users.ShellConfig{Tool: "bash"},
		Runtimes:    users.RuntimesConfig{Node: "none", Python: "none", Go: "none", Rust: "none"},
		Tools:       users.ToolsConfig{Extras: []string{"gh"}},
	}
	df := GenerateDockerfile(p, "hopbox-base:abc")
	if !strings.Contains(df, "cli.github.com/packages") {
		t.Error("gh selected: Dockerfile should reference cli.github.com/packages")
	}
	if !strings.Contains(df, "apt-get install -y gh") {
		t.Error("gh selected: Dockerfile should apt-get install gh")
	}
}

func TestGenerateDockerfileAtuin(t *testing.T) {
	p := users.Profile{
		Multiplexer: users.MultiplexerConfig{Tool: "none"},
		Editor:      users.EditorConfig{Tool: "none"},
		Shell:       users.ShellConfig{Tool: "bash"},
		Runtimes:    users.RuntimesConfig{Node: "none", Python: "none", Go: "none", Rust: "none"},
		Tools:       users.ToolsConfig{Extras: []string{"atuin"}},
	}
	df := GenerateDockerfile(p, "hopbox-base:abc")
	if !strings.Contains(df, "atuinsh/atuin") {
		t.Error("atuin selected: Dockerfile should reference atuinsh/atuin release")
	}
	if !strings.Contains(df, "unknown-linux-gnu") {
		t.Error("atuin selected: Dockerfile should fetch the musl-gnu release tarball")
	}
}

func TestGenerateDockerfileClaudeCodeOnly(t *testing.T) {
	p := users.Profile{
		Multiplexer: users.MultiplexerConfig{Tool: "none"},
		Editor:      users.EditorConfig{Tool: "none"},
		Shell:       users.ShellConfig{Tool: "bash"},
		Runtimes:    users.RuntimesConfig{Node: "none", Python: "none", Go: "none", Rust: "none"},
		Tools:       users.ToolsConfig{AI: []string{"claude-code"}},
	}
	df := GenerateDockerfile(p, "hopbox-base:abc")
	if !strings.Contains(df, "claude.ai/install.sh") {
		t.Error("claude-code selected: Dockerfile should run claude.ai installer")
	}
	if !strings.Contains(df, "mv /root/.local/bin/claude /usr/local/bin/claude") {
		t.Error("claude-code selected: Dockerfile should relocate claude binary to /usr/local/bin")
	}
	if strings.Contains(df, "deb.nodesource.com") {
		t.Error("claude-code only: Dockerfile must not install NodeSource")
	}
}

func TestGenerateDockerfileCodexAndGemini(t *testing.T) {
	p := users.Profile{
		Multiplexer: users.MultiplexerConfig{Tool: "none"},
		Editor:      users.EditorConfig{Tool: "none"},
		Shell:       users.ShellConfig{Tool: "bash"},
		Runtimes:    users.RuntimesConfig{Node: "none", Python: "none", Go: "none", Rust: "none"},
		Tools:       users.ToolsConfig{AI: []string{"codex", "gemini-cli"}},
	}
	df := GenerateDockerfile(p, "hopbox-base:abc")
	if !strings.Contains(df, "deb.nodesource.com/setup_lts.x") {
		t.Error("codex/gemini selected: Dockerfile should install NodeSource LTS")
	}
	if !strings.Contains(df, "@openai/codex") {
		t.Error("codex selected: Dockerfile should npm install @openai/codex")
	}
	if !strings.Contains(df, "@google/gemini-cli") {
		t.Error("gemini-cli selected: Dockerfile should npm install @google/gemini-cli")
	}
}

func TestGenerateDockerfileNoAI(t *testing.T) {
	p := users.DefaultProfile()
	df := GenerateDockerfile(p, "hopbox-base:abc")
	if strings.Contains(df, "claude.ai/install.sh") {
		t.Error("default profile: should not install Claude Code")
	}
	if strings.Contains(df, "deb.nodesource.com") {
		t.Error("default profile: should not install NodeSource")
	}
	if strings.Contains(df, "@openai/codex") {
		t.Error("default profile: should not install codex")
	}
}
