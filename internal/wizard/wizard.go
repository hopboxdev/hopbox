package wizard

import (
	"fmt"
	"io"

	"github.com/charmbracelet/huh"

	"github.com/hopboxdev/hopbox/internal/users"
)

// RunWizard presents the tool selection form over the SSH session.
// Takes a Profile as defaults (pre-filled). Returns the updated Profile.
func RunWizard(defaults users.Profile, in io.Reader, out io.Writer, width, height int) (users.Profile, error) {
	p := defaults

	toolOptions := []huh.Option[string]{
		huh.NewOption("fzf", "fzf"),
		huh.NewOption("ripgrep", "ripgrep"),
		huh.NewOption("fd", "fd"),
		huh.NewOption("bat", "bat"),
		huh.NewOption("lazygit", "lazygit"),
		huh.NewOption("direnv", "direnv"),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Terminal Multiplexer").
				Options(
					huh.NewOption("zellij", "zellij"),
					huh.NewOption("tmux", "tmux"),
				).
				Value(&p.Multiplexer.Tool),
		),
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
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("CLI Tools").
				Options(toolOptions...).
				Value(&p.Tools.Extras),
		),
	).WithInput(in).WithOutput(out).WithWidth(width).WithHeight(height)

	if err := form.Run(); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	return p, nil
}
