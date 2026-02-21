package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/hopboxdev/hopbox/internal/ui"
)

// Step defines one unit of work in a step runner.
type Step struct {
	Title string
	Run   func(ctx context.Context, sub func(string)) error
}

// stepDoneMsg is sent when a step's Run function completes.
type stepDoneMsg struct {
	index int
	err   error
}

// subStepMsg is sent by a step's sub callback to update the spinner text.
type subStepMsg struct {
	msg string
}

type stepRunner struct {
	ctx     context.Context
	cancel  context.CancelFunc
	steps   []Step
	current int
	done    []string // completed step messages (shown with checkmark)
	spinner spinner.Model
	subMsg  string // current message shown next to spinner
	err     error
	program *tea.Program
}

func (m *stepRunner) Init() tea.Cmd {
	m.subMsg = m.steps[0].Title
	return tea.Batch(m.spinner.Tick, m.runStep(0))
}

func (m *stepRunner) runStep(idx int) tea.Cmd {
	step := m.steps[idx]
	return func() tea.Msg {
		sub := func(msg string) {
			m.program.Send(subStepMsg{msg: msg})
		}
		err := step.Run(m.ctx, sub)
		return stepDoneMsg{index: idx, err: err}
	}
}

func (m *stepRunner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.err = context.Canceled
			return m, tea.Quit
		}

	case subStepMsg:
		m.subMsg = msg.msg
		return m, nil

	case stepDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.done = append(m.done, m.subMsg)
		m.current++
		if m.current >= len(m.steps) {
			return m, tea.Quit
		}
		m.subMsg = m.steps[m.current].Title
		return m, m.runStep(m.current)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *stepRunner) View() string {
	var b strings.Builder
	for _, msg := range m.done {
		b.WriteString(ui.StepOK(msg) + "\n")
	}
	if m.err != nil {
		b.WriteString(ui.StepFail(m.subMsg) + "\n")
	} else if m.current < len(m.steps) {
		b.WriteString(m.spinner.View() + " " + m.subMsg + "\n")
	}
	return b.String()
}

// RunSteps executes steps sequentially with animated spinner progress.
// Each step shows a braille-dot spinner that resolves to a checkmark on success
// or a cross on failure. Sub-step messages update the spinner text in place.
// Falls back to plain output if stdout is not a TTY.
func RunSteps(ctx context.Context, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runStepsPlain(ctx, steps)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ui.Yellow)

	m := &stepRunner{
		ctx:     ctx,
		cancel:  cancel,
		steps:   steps,
		spinner: s,
	}
	p := tea.NewProgram(m)
	m.program = p

	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	if r, ok := result.(*stepRunner); ok && r.err != nil {
		return r.err
	}
	return nil
}

// runStepsPlain runs steps without animation (non-TTY fallback).
func runStepsPlain(ctx context.Context, steps []Step) error {
	for _, step := range steps {
		msg := step.Title
		sub := func(s string) { msg = s }
		err := step.Run(ctx, sub)
		if err != nil {
			fmt.Println(ui.StepFail(msg))
			return err
		}
		fmt.Println(ui.StepOK(msg))
	}
	return nil
}
