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
	if !strings.Contains(df, "mise install node@lts") {
		t.Error("should install node lts")
	}
	if !strings.Contains(df, "mise install python@3.12") {
		t.Error("should install python 3.12")
	}
	if strings.Contains(df, "mise install go") {
		t.Error("should not install go when set to none")
	}
	if strings.Contains(df, "mise install rust") {
		t.Error("should not install rust when set to none")
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
	tag := UserImageTag("gandalf", "abc123def456")
	if tag != "hopbox-gandalf:abc123def456" {
		t.Errorf("got %q", tag)
	}
}
