package containers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/hopboxdev/hopbox/internal/users"
	"github.com/moby/go-archive"
)

// UserImageTag returns the Docker image tag for a user's personalized image.
func UserImageTag(username, profileHash string) string {
	return fmt.Sprintf("hopbox-%s:%s", username, profileHash)
}

// GenerateDockerfile produces Dockerfile content for a user profile layered on the base image.
func GenerateDockerfile(p users.Profile, baseTag string) string {
	var b strings.Builder

	// Architecture detection
	linuxArch := "x86_64"
	nvimArch := "x86_64"
	if runtime.GOARCH == "arm64" {
		linuxArch = "aarch64"
		nvimArch = "arm64"
	}

	fmt.Fprintf(&b, "FROM %s\n\nUSER root\n\n", baseTag)

	// Multiplexer
	switch p.Multiplexer.Tool {
	case "zellij":
		fmt.Fprintf(&b,
			"RUN ZELLIJ_VERSION=$(curl -s https://api.github.com/repos/zellij-org/zellij/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
				"curl -fsSL \"https://github.com/zellij-org/zellij/releases/download/v${ZELLIJ_VERSION}/zellij-%s-unknown-linux-musl.tar.gz\" | tar -xz -C /usr/local/bin/\n",
			linuxArch,
		)
	case "tmux":
		b.WriteString("RUN apt-get update && apt-get install -y tmux && rm -rf /var/lib/apt/lists/*\n")
	}

	// Editor
	switch p.Editor.Tool {
	case "neovim":
		b.WriteString(fmt.Sprintf(
			"RUN curl -fsSL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-%s.tar.gz | tar -xz -C /opt/ && ln -sf /opt/nvim-linux-%s/bin/nvim /usr/local/bin/nvim\n",
			nvimArch, nvimArch,
		))
	case "vim":
		b.WriteString("RUN apt-get update && apt-get install -y vim && rm -rf /var/lib/apt/lists/*\n")
	}

	// Shell
	switch p.Shell.Tool {
	case "zsh":
		b.WriteString("RUN apt-get update && apt-get install -y zsh && rm -rf /var/lib/apt/lists/*\n")
		b.WriteString("RUN echo 'eval \"$(mise activate zsh)\"' >> /home/dev/.zshrc\n")
	case "fish":
		b.WriteString("RUN apt-get update && apt-get install -y fish && rm -rf /var/lib/apt/lists/*\n")
		b.WriteString("RUN mkdir -p /home/dev/.config/fish && echo 'mise activate fish | source' >> /home/dev/.config/fish/config.fish\n")
	}

	// Extra tools via apt
	var aptTools []string
	var needFd, needBat bool
	for _, tool := range p.Tools.Extras {
		switch tool {
		case "ripgrep":
			aptTools = append(aptTools, "ripgrep")
		case "fd":
			aptTools = append(aptTools, "fd-find")
			needFd = true
		case "bat":
			aptTools = append(aptTools, "bat")
			needBat = true
		}
	}
	if len(aptTools) > 0 {
		b.WriteString(fmt.Sprintf("RUN apt-get update && apt-get install -y %s && rm -rf /var/lib/apt/lists/*\n",
			strings.Join(aptTools, " ")))
		if needFd {
			b.WriteString("RUN ln -sf $(which fdfind) /usr/local/bin/fd\n")
		}
		if needBat {
			b.WriteString("RUN ln -sf $(which batcat) /usr/local/bin/bat\n")
		}
	}

	// Extra tools via download
	for _, tool := range p.Tools.Extras {
		switch tool {
		case "fzf":
			b.WriteString(fmt.Sprintf(
				"RUN FZF_VERSION=$(curl -s https://api.github.com/repos/junegunn/fzf/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
					"curl -fsSL https://github.com/junegunn/fzf/releases/download/v${FZF_VERSION}/fzf-${FZF_VERSION}-linux_%s.tar.gz | tar -xz -C /usr/local/bin/\n",
				runtime.GOARCH,
			))
		case "lazygit":
			b.WriteString(fmt.Sprintf(
				"RUN LAZYGIT_VERSION=$(curl -s https://api.github.com/repos/jesseduffield/lazygit/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
					"curl -fsSL https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_%s.tar.gz | tar -xz -C /usr/local/bin/ lazygit\n",
				linuxArch,
			))
		case "direnv":
			b.WriteString("RUN curl -sfL https://direnv.net/install.sh | bash\n")
		}
	}

	// Switch to dev user for runtime installs via mise
	b.WriteString("\nUSER dev\nWORKDIR /home/dev\n\n")

	type rt struct {
		name    string
		version string
	}
	runtimes := []rt{
		{"node", p.Runtimes.Node},
		{"python", p.Runtimes.Python},
		{"go", p.Runtimes.Go},
		{"rust", p.Runtimes.Rust},
	}
	for _, r := range runtimes {
		if r.version != "" && r.version != "none" {
			fmt.Fprintf(&b, "RUN mise install %s@%s && mise use --global %s@%s\n",
				r.name, r.version, r.name, r.version)
		}
	}

	b.WriteString("\nCMD [\"sleep\", \"infinity\"]\n")

	return b.String()
}

// EnsureUserImage checks if the per-user image exists and builds it if not.
// If progress is non-nil, build step summaries are written to it (e.g. the SSH session).
func EnsureUserImage(ctx context.Context, cli *client.Client, username string, p users.Profile, baseTag string, progress io.Writer) (string, error) {
	tag := UserImageTag(username, p.Hash())

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

	// Generate Dockerfile and build
	df := GenerateDockerfile(p, baseTag)

	tmpDir, err := os.MkdirTemp("", "hopbox-userbuild-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(df), 0644); err != nil {
		return "", fmt.Errorf("write Dockerfile: %w", err)
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

	// Parse build output, check for errors, stream progress
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
			// Show Docker build steps to the user
			if progress != nil && strings.HasPrefix(msg.Stream, "Step ") {
				fmt.Fprintf(progress, "  %s", msg.Stream)
			}
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
