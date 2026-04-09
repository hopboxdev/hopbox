package wizard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"

	"github.com/hopboxdev/hopbox/internal/users"
)

type step int

const (
	stepMux step = iota
	stepEditor
	stepShell
	stepRuntimes
	stepTools
	stepDone
)

type wizardModel struct {
	step    step
	form    *huh.Form
	profile users.Profile
	aborted bool
}

func newWizardModel(defaults users.Profile) wizardModel {
	m := wizardModel{profile: defaults}
	m.form = m.buildForm(stepMux)
	return m
}

func (m wizardModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateAborted {
		m.aborted = true
		return m, tea.Quit
	}

	if m.form.State == huh.StateCompleted {
		m.step++
		if m.step >= stepDone {
			return m, tea.Quit
		}
		m.form = m.buildForm(m.step)
		return m, m.form.Init()
	}

	return m, cmd
}

func (m wizardModel) View() string {
	if m.step >= stepDone {
		return ""
	}
	return m.form.View()
}

func (m wizardModel) buildForm(s step) *huh.Form {
	switch s {
	case stepMux:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Terminal Multiplexer").
				Options(
					huh.NewOption("zellij", "zellij"),
					huh.NewOption("tmux", "tmux"),
				).Value(&m.profile.Multiplexer.Tool),
		))
	case stepEditor:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Editor").
				Options(
					huh.NewOption("neovim", "neovim"),
					huh.NewOption("vim", "vim"),
					huh.NewOption("none", "none"),
				).Value(&m.profile.Editor.Tool),
		))
	case stepShell:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Shell").
				Options(
					huh.NewOption("bash", "bash"),
					huh.NewOption("zsh", "zsh"),
					huh.NewOption("fish", "fish"),
				).Value(&m.profile.Shell.Tool),
		))
	case stepRuntimes:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Node.js").
				Options(
					huh.NewOption("LTS", "lts"),
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Node),
			huh.NewSelect[string]().
				Title("Python").
				Options(
					huh.NewOption("3.12", "3.12"),
					huh.NewOption("3.13", "3.13"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Python),
			huh.NewSelect[string]().
				Title("Go").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Go),
			huh.NewSelect[string]().
				Title("Rust").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Rust),
		))
	case stepTools:
		return huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("CLI Tools").
				Options(
					huh.NewOption("fzf", "fzf"),
					huh.NewOption("ripgrep", "ripgrep"),
					huh.NewOption("fd", "fd"),
					huh.NewOption("bat", "bat"),
					huh.NewOption("lazygit", "lazygit"),
					huh.NewOption("direnv", "direnv"),
				).Value(&m.profile.Tools.Extras),
		))
	default:
		return huh.NewForm(huh.NewGroup())
	}
}

// RunWizard presents the tool selection wizard over the SSH session.
// Uses a single tea.Program to avoid cancel reader races between forms.
func RunWizard(defaults users.Profile, sess ssh.Session) (users.Profile, error) {
	pty, winCh, ok := sess.Pty()
	if !ok {
		return defaults, fmt.Errorf("no PTY available")
	}

	m := newWizardModel(defaults)
	opts := append(bubbletea.MakeOptions(sess), tea.WithAltScreen())
	p := tea.NewProgram(m, opts...)

	// Send initial window size
	go func() {
		p.Send(tea.WindowSizeMsg{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		})
	}()

	// Forward window resizes until program exits
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case w, ok := <-winCh:
				if !ok {
					return
				}
				p.Send(tea.WindowSizeMsg{
					Width:  w.Width,
					Height: w.Height,
				})
			}
		}
	}()

	result, err := p.Run()
	close(done)
	if err != nil {
		return defaults, fmt.Errorf("wizard: %w", err)
	}

	wm := result.(wizardModel)
	if wm.aborted {
		return defaults, fmt.Errorf("wizard cancelled")
	}
	return wm.profile, nil
}
