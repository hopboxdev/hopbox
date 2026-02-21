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

// Phase groups related steps under a visual header.
type Phase struct {
	Title string
	Steps []Step
}

// Step defines one unit of work in a phased runner.
type Step struct {
	Title    string
	Run      func(ctx context.Context, send func(StepEvent)) error
	NonFatal bool // if true, error renders as warning and execution continues
}

// StepEvent lets a running step report progress updates.
type StepEvent struct {
	Message string
}

type status int

const (
	statusPending status = iota
	statusRunning
	statusDone
	statusFailed
	statusWarned
)

type flatStep struct {
	phaseIdx int
	title    string
	status   status
	message  string // last StepEvent message (shown while running)
	errMsg   string // error or warning detail
	nonFatal bool
}

// Internal Bubble Tea messages.
type stepEventMsg struct{ message string }
type stepDoneMsg struct{}
type stepFailMsg struct{ err error }

type runner struct {
	title      string
	phases     []Phase
	steps      []flatStep
	current    int
	totalSteps int
	spinner    spinner.Model
	err        error
	done       bool
	program    *tea.Program
	ctx        context.Context
	cancel     context.CancelFunc
}

func (m *runner) Init() tea.Cmd {
	if len(m.steps) == 0 {
		m.done = true
		return tea.Quit
	}
	m.steps[0].status = statusRunning
	return tea.Batch(m.spinner.Tick, m.runCurrentStep())
}

func (m *runner) runCurrentStep() tea.Cmd {
	idx := m.current

	// Find the original Step to get the Run function.
	var originalStep Step
	flatIdx := 0
	for _, p := range m.phases {
		for _, s := range p.Steps {
			if flatIdx == idx {
				originalStep = s
				break
			}
			flatIdx++
		}
		if flatIdx == idx {
			break
		}
	}

	return func() tea.Msg {
		defer func() {
			if r := recover(); r != nil {
				m.program.Send(stepFailMsg{err: fmt.Errorf("panic: %v", r)})
			}
		}()
		send := func(evt StepEvent) {
			m.program.Send(stepEventMsg{message: evt.Message})
		}
		err := originalStep.Run(m.ctx, send)
		if err != nil {
			return stepFailMsg{err: err}
		}
		return stepDoneMsg{}
	}
}

func (m *runner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.err = context.Canceled
			return m, tea.Quit
		}

	case stepEventMsg:
		if m.current < len(m.steps) {
			m.steps[m.current].message = msg.message
		}
		return m, nil

	case stepDoneMsg:
		m.steps[m.current].status = statusDone
		m.current++
		if m.current >= len(m.steps) {
			m.done = true
			return m, tea.Quit
		}
		m.steps[m.current].status = statusRunning
		return m, m.runCurrentStep()

	case stepFailMsg:
		step := &m.steps[m.current]
		if step.nonFatal {
			step.status = statusWarned
			step.errMsg = msg.err.Error()
			m.current++
			if m.current >= len(m.steps) {
				m.done = true
				return m, tea.Quit
			}
			m.steps[m.current].status = statusRunning
			return m, m.runCurrentStep()
		}
		step.status = statusFailed
		step.errMsg = msg.err.Error()
		m.err = msg.err
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

var (
	titleStyle       = lipgloss.NewStyle().Bold(true)
	counterStyle     = lipgloss.NewStyle().Foreground(ui.Subtle)
	phaseHeaderStyle = lipgloss.NewStyle().Bold(true)
	pendingStyle     = lipgloss.NewStyle().Foreground(ui.Subtle)
	errorStyle       = lipgloss.NewStyle().Foreground(ui.Red)
	warnStyle        = lipgloss.NewStyle().Foreground(ui.Yellow)
)

func (m *runner) View() string {
	var b strings.Builder

	// Title with step counter.
	completedCount := 0
	for _, s := range m.steps {
		if s.status == statusDone || s.status == statusWarned {
			completedCount++
		}
	}
	if m.err != nil || (!m.done && m.current < len(m.steps)) {
		completedCount = m.current
	}
	counter := counterStyle.Render(fmt.Sprintf(" [%d/%d]", completedCount, m.totalSteps))
	b.WriteString(titleStyle.Render(m.title) + counter + "\n")

	// Render phases and steps.
	lastPhaseIdx := -1
	for _, step := range m.steps {
		// Insert phase header when phase changes.
		if step.phaseIdx != lastPhaseIdx {
			lastPhaseIdx = step.phaseIdx
			b.WriteString("\n" + phaseHeaderStyle.Render(m.phases[step.phaseIdx].Title) + "\n")
		}

		switch step.status {
		case statusDone:
			msg := step.title
			if step.message != "" {
				msg = step.message
			}
			b.WriteString("  " + ui.StepOK(msg) + "\n")
		case statusWarned:
			b.WriteString("  " + ui.Warn(step.title) + "\n")
			if step.errMsg != "" {
				b.WriteString("    " + warnStyle.Render("Warning: "+step.errMsg) + "\n")
			}
		case statusRunning:
			msg := step.title
			if step.message != "" {
				msg = step.message
			}
			b.WriteString("  " + m.spinner.View() + " " + msg + "\n")
		case statusFailed:
			b.WriteString("  " + ui.StepFail(step.title) + "\n")
			if step.errMsg != "" {
				b.WriteString("    " + errorStyle.Render("Error: "+step.errMsg) + "\n")
			}
		case statusPending:
			b.WriteString("  " + pendingStyle.Render("○ "+step.title) + "\n")
		}
	}

	return b.String()
}

// RunPhases executes phases sequentially, rendering progress.
// Returns error if any step fails (unless NonFatal).
// Falls back to plain output if stdout is not a TTY.
func RunPhases(ctx context.Context, title string, phases []Phase) error {
	// Filter out empty phases.
	var nonEmpty []Phase
	for _, p := range phases {
		if len(p.Steps) > 0 {
			nonEmpty = append(nonEmpty, p)
		}
	}
	phases = nonEmpty

	if len(phases) == 0 {
		return nil
	}

	// Count total steps.
	total := 0
	for _, p := range phases {
		total += len(p.Steps)
	}

	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runPhasesPlain(ctx, title, phases)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Flatten steps.
	var flat []flatStep
	for pi, p := range phases {
		for _, s := range p.Steps {
			flat = append(flat, flatStep{
				phaseIdx: pi,
				title:    s.Title,
				status:   statusPending,
				nonFatal: s.NonFatal,
			})
		}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ui.Yellow)

	m := &runner{
		title:      title,
		phases:     phases,
		steps:      flat,
		totalSteps: total,
		spinner:    s,
		ctx:        ctx,
		cancel:     cancel,
	}
	p := tea.NewProgram(m)
	m.program = p

	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	if r, ok := result.(*runner); ok && r.err != nil {
		return r.err
	}
	return nil
}

// runPhasesPlain runs phases without animation (non-TTY fallback).
func runPhasesPlain(ctx context.Context, title string, phases []Phase) error {
	fmt.Println(title)
	for _, phase := range phases {
		for _, step := range phase.Steps {
			msg := step.Title
			send := func(evt StepEvent) { msg = evt.Message }
			err := step.Run(ctx, send)
			if err != nil {
				if step.NonFatal {
					fmt.Println("  " + ui.Warn(msg))
					continue
				}
				fmt.Println("  " + ui.StepFail(msg))
				return err
			}
			fmt.Println("  " + ui.StepOK(msg))
		}
	}
	return nil
}
