package containers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/hopboxdev/hopbox/internal/metrics"
	"github.com/hopboxdev/hopbox/internal/users"
	"github.com/moby/go-archive"
)

// UserImageTag returns the Docker image tag for a user's personalized image.
// Includes the base tag hash so base image changes invalidate user images.
func UserImageTag(username, profileHash, baseTag string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s:%s", profileHash, baseTag)
	combined := hex.EncodeToString(h.Sum(nil))[:12]
	return fmt.Sprintf("hopbox-%s:%s", username, combined)
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
	hasDocker := false
	for _, tool := range p.Tools.Extras {
		switch tool {
		case "fzf":
			b.WriteString(fmt.Sprintf(
				"RUN FZF_VERSION=$(curl -s https://api.github.com/repos/junegunn/fzf/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
					"curl -fsSL https://github.com/junegunn/fzf/releases/download/v${FZF_VERSION}/fzf-${FZF_VERSION}-linux_%s.tar.gz | tar -xz -C /usr/local/bin/\n",
				runtime.GOARCH,
			))
		case "lazygit":
			lgArch := runtime.GOARCH // "amd64" or "arm64" — matches lazygit naming
			if lgArch == "amd64" {
				lgArch = "x86_64"
			}
			fmt.Fprintf(&b,
				"RUN LAZYGIT_VERSION=$(curl -s https://api.github.com/repos/jesseduffield/lazygit/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
					"curl -fsSL \"https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_linux_%s.tar.gz\" | tar -xz -C /usr/local/bin/ lazygit\n",
				lgArch,
			)
		case "direnv":
			b.WriteString("RUN curl -sfL https://direnv.net/install.sh | bash\n")
		case "docker":
			b.WriteString("RUN apt-get update && " +
				"install -m 0755 -d /etc/apt/keyrings && " +
				"curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && " +
				"echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable\" > /etc/apt/sources.list.d/docker.list && " +
				"apt-get update && apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin && " +
				"rm -rf /var/lib/apt/lists/* && " +
				"usermod -aG docker dev\n")
			hasDocker = true
		case "gh":
			b.WriteString("RUN mkdir -p /etc/apt/keyrings && " +
				"curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
				"chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && " +
				"echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\" > /etc/apt/sources.list.d/github-cli.list && " +
				"apt-get update && apt-get install -y gh && rm -rf /var/lib/apt/lists/*\n")
		case "atuin":
			fmt.Fprintf(&b,
				"RUN ATUIN_VERSION=$(curl -s https://api.github.com/repos/atuinsh/atuin/releases/latest | grep tag_name | cut -d '\"' -f4 | tr -d 'v') && "+
					"curl -fsSL \"https://github.com/atuinsh/atuin/releases/download/v${ATUIN_VERSION}/atuin-%s-unknown-linux-gnu.tar.gz\" | "+
					"tar -xz --strip-components=1 -C /usr/local/bin/ atuin-%s-unknown-linux-gnu/atuin\n",
				linuxArch, linuxArch,
			)
		}
	}

	// Install nvm if requested (before switching to dev user)
	if p.Runtimes.Node == "nvm" {
		b.WriteString("ENV NVM_DIR=/opt/nvm\n")
		b.WriteString("RUN mkdir -p /opt/nvm && curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | bash && chown -R dev:dev /opt/nvm\n")
		b.WriteString("RUN echo 'export NVM_DIR=/opt/nvm && [ -s \"$NVM_DIR/nvm.sh\" ] && . \"$NVM_DIR/nvm.sh\"' >> /etc/bash.bashrc\n")
	}

	// AI tools (run as root, install to /usr/local/bin so bind-mounted /home/dev doesn't hide them)
	if len(p.Tools.AI) > 0 {
		needNode := false
		var npmPkgs []string
		for _, tool := range p.Tools.AI {
			switch tool {
			case "claude-code":
				b.WriteString("RUN curl -fsSL https://claude.ai/install.sh | bash && " +
					"mv /root/.local/bin/claude /usr/local/bin/claude\n")
			case "codex":
				needNode = true
				npmPkgs = append(npmPkgs, "@openai/codex")
			case "gemini-cli":
				needNode = true
				npmPkgs = append(npmPkgs, "@google/gemini-cli")
			}
		}
		if needNode {
			b.WriteString("RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - && " +
				"apt-get update && apt-get install -y nodejs && rm -rf /var/lib/apt/lists/*\n")
			fmt.Fprintf(&b, "RUN npm install -g %s\n", strings.Join(npmPkgs, " "))
		}
	}

	// Switch to dev user for runtime installs via mise
	b.WriteString("\nUSER dev\nWORKDIR /home/dev\n\n")

	type rt struct {
		name    string
		version string
	}
	runtimes := []rt{
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

	if hasDocker {
		// Start dockerd in the background before sleeping.
		// Sysbox provides the isolation needed for nested Docker.
		b.WriteString("\nUSER root\n")
		b.WriteString("RUN printf '#!/bin/sh\\ndockerd > /var/log/dockerd.log 2>&1 &\\nsleep 3\\nexec su -s /bin/sh dev -c \"sleep infinity\"\\n' > /usr/local/bin/hopbox-entry.sh && chmod +x /usr/local/bin/hopbox-entry.sh\n")
		b.WriteString("CMD [\"/usr/local/bin/hopbox-entry.sh\"]\n")
	} else {
		b.WriteString("\nCMD [\"sleep\", \"infinity\"]\n")
	}

	return b.String()
}

// EnsureUserImage checks if the per-user image exists and builds it if not.
func EnsureUserImage(ctx context.Context, cli *client.Client, username string, p users.Profile, baseTag string) (string, error) {
	tag := UserImageTag(username, p.Hash(), baseTag)

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
	start := time.Now()
	defer func() {
		metrics.BuildDurationSeconds.Observe(time.Since(start).Seconds())
	}()
	slog.Info("building user image", "component", "builder", "tag", tag, "user", username)
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

	// Drain build output, check for errors
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
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
