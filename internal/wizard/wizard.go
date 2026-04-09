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
	stepUsername step = iota
	stepMux
	stepEditor
	stepShell
	stepNode
	stepPython
	stepGo
	stepRust
	stepTools
	stepDone
)

// Result holds the wizard output.
type Result struct {
	Username string // only set if registration was included
	Profile  users.Profile
}

type wizardModel struct {
	step     step
	firstStep step
	form     *huh.Form
	profile  users.Profile
	username string
	validateUsername func(string) error
	aborted  bool
}

func (m wizardModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Esc goes back one step
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEscape && m.step > m.firstStep {
			m.step--
			m.form = m.buildForm(m.step)
			return m, m.form.Init()
		}
	}

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
	case stepUsername:
		return huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Welcome to Hopbox!").
				Description("Choose a username for your dev environment.").
				Placeholder("username").
				Value(&m.username).
				Validate(m.validateUsername),
		))
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
	case stepNode:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Node.js").
				Options(
					huh.NewOption("LTS", "lts"),
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Node),
		))
	case stepPython:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Python").
				Options(
					huh.NewOption("3.12", "3.12"),
					huh.NewOption("3.13", "3.13"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Python),
		))
	case stepGo:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Go").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.profile.Runtimes.Go),
		))
	case stepRust:
		return huh.NewForm(huh.NewGroup(
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

func runProgram(sess ssh.Session, model tea.Model) (tea.Model, error) {
	pty, winCh, ok := sess.Pty()
	if !ok {
		return nil, fmt.Errorf("no PTY available")
	}

	p := tea.NewProgram(model, bubbletea.MakeOptions(sess)...)

	go func() {
		p.Send(tea.WindowSizeMsg{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		})
	}()

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
	return result, err
}

// RunSetup runs registration + tool selection as a single tea.Program.
// If needsRegistration is true, the username step is included.
func RunSetup(defaults users.Profile, sess ssh.Session, needsRegistration bool, validateUsername func(string) error) (*Result, error) {
	firstStep := stepMux
	if needsRegistration {
		firstStep = stepUsername
	}

	m := wizardModel{
		step:            firstStep,
		firstStep:       firstStep,
		profile:         defaults,
		validateUsername: validateUsername,
	}
	m.form = m.buildForm(m.step)

	result, err := runProgram(sess, m)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	wm := result.(wizardModel)
	if wm.aborted {
		return nil, fmt.Errorf("setup cancelled")
	}
	return &Result{
		Username: wm.username,
		Profile:  wm.profile,
	}, nil
}

// RunWizard runs just the tool selection wizard (no registration step).
func RunWizard(defaults users.Profile, sess ssh.Session) (users.Profile, error) {
	result, err := RunSetup(defaults, sess, false, nil)
	if err != nil {
		return defaults, err
	}
	return result.Profile, nil
}
