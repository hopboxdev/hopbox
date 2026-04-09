package wizard

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"

	"github.com/hopboxdev/hopbox/internal/users"
)

// runForm runs a single huh form over the SSH session.
func runForm(sess ssh.Session, fields ...huh.Field) error {
	form := huh.NewForm(
		huh.NewGroup(fields...),
	).WithProgramOptions(bubbletea.MakeOptions(sess)...)

	return form.Run()
}

// RunWizard presents the tool selection form over the SSH session.
// Each category is a separate form to avoid rendering issues with multi-group forms over SSH.
func RunWizard(defaults users.Profile, sess ssh.Session) (users.Profile, error) {
	p := defaults

	// Multiplexer
	if err := runForm(sess,
		huh.NewSelect[string]().
			Title("Terminal Multiplexer").
			Options(
				huh.NewOption("zellij", "zellij"),
				huh.NewOption("tmux", "tmux"),
			).
			Filtering(false).
			Value(&p.Multiplexer.Tool),
	); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	// Editor
	if err := runForm(sess,
		huh.NewSelect[string]().
			Title("Editor").
			Options(
				huh.NewOption("neovim", "neovim"),
				huh.NewOption("vim", "vim"),
				huh.NewOption("none", "none"),
			).
			Filtering(false).
			Value(&p.Editor.Tool),
	); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	// Shell
	if err := runForm(sess,
		huh.NewSelect[string]().
			Title("Shell").
			Options(
				huh.NewOption("bash", "bash"),
				huh.NewOption("zsh", "zsh"),
				huh.NewOption("fish", "fish"),
			).
			Filtering(false).
			Value(&p.Shell.Tool),
	); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	// Runtimes
	if err := runForm(sess,
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
	); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	// CLI Tools
	if err := runForm(sess,
		huh.NewMultiSelect[string]().
			Title("CLI Tools").
			Options(
				huh.NewOption("fzf", "fzf"),
				huh.NewOption("ripgrep", "ripgrep"),
				huh.NewOption("fd", "fd"),
				huh.NewOption("bat", "bat"),
				huh.NewOption("lazygit", "lazygit"),
				huh.NewOption("direnv", "direnv"),
			).
			Value(&p.Tools.Extras),
	); err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	return p, nil
}
