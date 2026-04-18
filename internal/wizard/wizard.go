package wizard

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
)

type step int

const (
	stepChoice step = iota
	stepLinkCode
	stepUsername
	stepDone
)

// Result holds the wizard output.
type Result struct {
	Username string // set iff registration happened
	LinkMode bool   // true iff user chose "link to existing account"
	LinkCode string
}

type wizardData struct {
	Username string
	Choice   string // "create" or "link"
	LinkCode string
}

type wizardModel struct {
	step             step
	firstStep        step
	form             *huh.Form
	data             *wizardData
	validateUsername func(string) error
	aborted          bool
}

func (m wizardModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEscape && m.step > m.firstStep {
			m.step--
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
		if m.step == stepChoice {
			if m.data.Choice == "link" {
				m.step = stepLinkCode
			} else {
				m.step = stepUsername
			}
			m.form = m.buildForm(m.step)
			return m, m.form.Init()
		}
		if m.step == stepLinkCode || m.step == stepUsername {
			m.step = stepDone
			return m, tea.Quit
		}
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
	default:
		return huh.NewForm(huh.NewGroup())
	}
}

func runProgram(sess ssh.Session, model tea.Model) (tea.Model, error) {
	pty, winCh, ok := sess.Pty()
	if !ok {
		return nil, fmt.Errorf("no PTY available")
	}

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

// RunSetup prompts for registration or key-linking. No tool or shell picker.
// needsRegistration=true shows the full picker; false-mode is unused today but
// reserved for future per-box setup flows.
func RunSetup(sess ssh.Session, needsRegistration bool, validateUsername func(string) error) (*Result, error) {
	firstStep := stepUsername
	if needsRegistration {
		firstStep = stepChoice
	}
	data := &wizardData{}
	m := wizardModel{
		step:             firstStep,
		firstStep:        firstStep,
		data:             data,
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
		LinkMode: data.Choice == "link",
		LinkCode: data.LinkCode,
	}, nil
}
