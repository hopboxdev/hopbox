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
	stepChoice   step = iota
	stepLinkCode
	stepUsername
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
	LinkMode bool   // true if user chose "link to existing account"
	LinkCode string // the code entered by the user
}

// wizardData is shared via pointer so huh form Value() bindings persist
// across bubbletea's model copies.
type wizardData struct {
	Profile  users.Profile
	Username string
	Choice   string // "create" or "link"
	LinkCode string
}

type wizardModel struct {
	step            step
	firstStep       step
	form            *huh.Form
	data            *wizardData
	validateUsername func(string) error
	aborted         bool
}

func (m wizardModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Esc goes back one step
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEscape && m.step > m.firstStep {
			m.step--
			// Skip stepLinkCode when going back if choice was "create"
			if m.step == stepLinkCode && m.data.Choice == "create" {
				m.step = stepChoice
			}
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
		// Handle choice-based transitions
		if m.step == stepChoice {
			if m.data.Choice == "link" {
				m.step = stepLinkCode
			} else {
				m.step = stepUsername
			}
			m.form = m.buildForm(m.step)
			return m, m.form.Init()
		}

		if m.step == stepLinkCode {
			// Link code entered, we're done
			m.step = stepDone
			return m, tea.Quit
		}

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
	case stepChoice:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Welcome to Hopbox!").
				Description("Choose how to get started.").
				Options(
					huh.NewOption("Create new account", "create"),
					huh.NewOption("Link to existing account", "link"),
				).Value(&m.data.Choice),
		))
	case stepLinkCode:
		return huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Link Code").
				Description("Enter the code from `hopbox link` on your other device.").
				Placeholder("XXXX-XXXX").
				Value(&m.data.LinkCode).
				Validate(func(s string) error {
					if len(s) != 9 || s[4] != '-' {
						return fmt.Errorf("code must be in XXXX-XXXX format")
					}
					return nil
				}),
		))
	case stepUsername:
		return huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Welcome to Hopbox!").
				Description("Choose a username for your dev environment.").
				Placeholder("username").
				Value(&m.data.Username).
				Validate(m.validateUsername),
		))
	case stepMux:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Terminal Multiplexer").
				Options(
					huh.NewOption("zellij", "zellij"),
					huh.NewOption("tmux", "tmux"),
					huh.NewOption("none", "none"),
				).Value(&m.data.Profile.Multiplexer.Tool),
		))
	case stepEditor:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Editor").
				Options(
					huh.NewOption("neovim", "neovim"),
					huh.NewOption("vim", "vim"),
					huh.NewOption("none", "none"),
				).Value(&m.data.Profile.Editor.Tool),
		))
	case stepShell:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Shell").
				Options(
					huh.NewOption("bash", "bash"),
					huh.NewOption("zsh", "zsh"),
					huh.NewOption("fish", "fish"),
				).Value(&m.data.Profile.Shell.Tool),
		))
	case stepNode:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Node.js (nvm)").
				Options(
					huh.NewOption("Install nvm", "nvm"),
					huh.NewOption("None", "none"),
				).Value(&m.data.Profile.Runtimes.Node),
		))
	case stepPython:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Python").
				Options(
					huh.NewOption("3.12", "3.12"),
					huh.NewOption("3.13", "3.13"),
					huh.NewOption("None", "none"),
				).Value(&m.data.Profile.Runtimes.Python),
		))
	case stepGo:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Go").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.data.Profile.Runtimes.Go),
		))
	case stepRust:
		return huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Rust").
				Options(
					huh.NewOption("Latest", "latest"),
					huh.NewOption("None", "none"),
				).Value(&m.data.Profile.Runtimes.Rust),
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
					huh.NewOption("docker", "docker"),
					huh.NewOption("gh (GitHub CLI)", "gh"),
					huh.NewOption("atuin", "atuin"),
				).Value(&m.data.Profile.Tools.Extras),
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

	// wish/bubbletea.MakeOptions wires input/output but does not propagate
	// the pty's TERM from the ssh pty-req into the program environment.
	// Without TERM, termenv falls back to ASCII mode: no colors, and some
	// huh widgets misrender on input. Inject TERM explicitly.
	env := append(sess.Environ(), "TERM="+pty.Term)
	opts := append(bubbletea.MakeOptions(sess), tea.WithEnvironment(env))
	p := tea.NewProgram(model, opts...)

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
		firstStep = stepChoice
	}

	data := &wizardData{Profile: defaults}
	m := wizardModel{
		step:            firstStep,
		firstStep:       firstStep,
		data:            data,
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
		Username: data.Username,
		Profile:  data.Profile,
		LinkMode: data.Choice == "link",
		LinkCode: data.LinkCode,
	}, nil
}
