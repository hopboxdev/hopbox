# TUI Revamp Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the minimal step runner with a phased progress display showing nested sub-steps, step counter, non-fatal warnings, and Charm-style rendering.

**Architecture:** New `internal/tui/runner.go` Bubble Tea model with `Phase`/`Step` types. Steps flatten into a sequential list; `phaseIdx` drives grouping in the view. Each CLI command migrates from `tui.RunSteps` to `tui.RunPhases`. The `internal/setup` package's `OnStep func(string)` callback changes to `func(StepEvent)`.

**Tech Stack:** Go, Bubble Tea, Bubbles (spinner), Lipgloss, golang.org/x/term.

---

### Task 1: Build the new runner with tests

**Files:**
- Create: `internal/tui/runner.go`
- Create: `internal/tui/runner_test.go`

**Step 1: Write the types and `RunPhases` function**

Create `internal/tui/runner.go` with the full implementation:

```go
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
	step := m.steps[idx]
	phase := m.phases[step.phaseIdx]
	_ = phase // phase context available if needed

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
		// Show current progress (1-indexed for the step being worked on).
		completedCount = m.current
		for i := 0; i < m.current; i++ {
			_ = i // already counted
		}
	}
	counter := counterStyle.Render(fmt.Sprintf(" [%d/%d]", completedCount, m.totalSteps))
	b.WriteString(titleStyle.Render(m.title) + counter + "\n")

	// Render phases and steps.
	lastPhaseIdx := -1
	for i, step := range m.steps {
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
			_ = i
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
```

**Step 2: Write tests for the runner**

Create `internal/tui/runner_test.go`:

```go
package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func newTestRunner(phases []Phase) *runner {
	var flat []flatStep
	total := 0
	for pi, p := range phases {
		for _, s := range p.Steps {
			flat = append(flat, flatStep{
				phaseIdx: pi,
				title:    s.Title,
				status:   statusPending,
				nonFatal: s.NonFatal,
			})
			total++
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &runner{
		title:      "Test",
		phases:     phases,
		steps:      flat,
		totalSteps: total,
		spinner:    spinner.New(),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func noop(ctx context.Context, send func(StepEvent)) error { return nil }

func TestRunnerInit(t *testing.T) {
	phases := []Phase{
		{Title: "Phase 1", Steps: []Step{
			{Title: "step 1", Run: noop},
			{Title: "step 2", Run: noop},
		}},
	}
	m := newTestRunner(phases)
	m.Init()
	if m.steps[0].status != statusRunning {
		t.Errorf("first step status = %d, want statusRunning", m.steps[0].status)
	}
	if m.current != 0 {
		t.Errorf("current = %d, want 0", m.current)
	}
}

func TestRunnerInitEmpty(t *testing.T) {
	m := newTestRunner(nil)
	m.Init()
	if !m.done {
		t.Error("empty runner should be done after Init")
	}
}

func TestRunnerStepEvent(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{{Title: "s1", Run: noop}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusRunning
	model, cmd := m.Update(stepEventMsg{message: "doing work"})
	r := model.(*runner)
	if r.steps[0].message != "doing work" {
		t.Errorf("message = %q, want %q", r.steps[0].message, "doing work")
	}
	if cmd != nil {
		t.Error("stepEventMsg should return nil cmd")
	}
}

func TestRunnerStepDone(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "s1", Run: noop},
			{Title: "s2", Run: noop},
		}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusRunning
	model, _ := m.Update(stepDoneMsg{})
	r := model.(*runner)
	if r.steps[0].status != statusDone {
		t.Errorf("step 0 status = %d, want statusDone", r.steps[0].status)
	}
	if r.current != 1 {
		t.Errorf("current = %d, want 1", r.current)
	}
	if r.steps[1].status != statusRunning {
		t.Errorf("step 1 status = %d, want statusRunning", r.steps[1].status)
	}
}

func TestRunnerLastStepDone(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{{Title: "only", Run: noop}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusRunning
	model, _ := m.Update(stepDoneMsg{})
	r := model.(*runner)
	if !r.done {
		t.Error("runner should be done after last step")
	}
	if r.err != nil {
		t.Errorf("err = %v, want nil", r.err)
	}
}

func TestRunnerStepFail(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{{Title: "bad", Run: noop}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusRunning
	testErr := errors.New("boom")
	model, _ := m.Update(stepFailMsg{err: testErr})
	r := model.(*runner)
	if r.steps[0].status != statusFailed {
		t.Errorf("status = %d, want statusFailed", r.steps[0].status)
	}
	if r.err != testErr {
		t.Errorf("err = %v, want %v", r.err, testErr)
	}
	if r.steps[0].errMsg != "boom" {
		t.Errorf("errMsg = %q, want %q", r.steps[0].errMsg, "boom")
	}
}

func TestRunnerNonFatalStep(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "warn", Run: noop, NonFatal: true},
			{Title: "next", Run: noop},
		}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusRunning
	model, _ := m.Update(stepFailMsg{err: errors.New("not critical")})
	r := model.(*runner)
	if r.steps[0].status != statusWarned {
		t.Errorf("status = %d, want statusWarned", r.steps[0].status)
	}
	if r.current != 1 {
		t.Errorf("current = %d, want 1 (should advance past non-fatal)", r.current)
	}
	if r.err != nil {
		t.Errorf("err = %v, want nil (non-fatal should not set err)", r.err)
	}
}

func TestRunnerViewPhaseHeaders(t *testing.T) {
	phases := []Phase{
		{Title: "Alpha", Steps: []Step{{Title: "a1", Run: noop}}},
		{Title: "Beta", Steps: []Step{{Title: "b1", Run: noop}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusDone
	m.steps[0].message = "a1 done"
	m.steps[1].status = statusPending
	m.current = 1
	view := m.View()
	if !strings.Contains(view, "Alpha") {
		t.Errorf("view should contain phase header 'Alpha', got %q", view)
	}
	if !strings.Contains(view, "Beta") {
		t.Errorf("view should contain phase header 'Beta', got %q", view)
	}
}

func TestRunnerViewStepCounter(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "s1", Run: noop},
			{Title: "s2", Run: noop},
			{Title: "s3", Run: noop},
		}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusDone
	m.steps[1].status = statusRunning
	m.steps[2].status = statusPending
	m.current = 1
	view := m.View()
	if !strings.Contains(view, "[1/3]") {
		t.Errorf("view should contain step counter [1/3], got %q", view)
	}
}

func TestRunnerViewError(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{{Title: "bad", Run: noop}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusFailed
	m.steps[0].errMsg = "something broke"
	m.err = errors.New("something broke")
	view := m.View()
	if !strings.Contains(view, "bad") {
		t.Errorf("view should show failed step title, got %q", view)
	}
	if !strings.Contains(view, "something broke") {
		t.Errorf("view should show error message, got %q", view)
	}
}

func TestRunnerViewWarning(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{{Title: "warn step", Run: noop, NonFatal: true}}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusWarned
	m.steps[0].errMsg = "not critical"
	view := m.View()
	if !strings.Contains(view, "warn step") {
		t.Errorf("view should show warned step, got %q", view)
	}
	if !strings.Contains(view, "not critical") {
		t.Errorf("view should show warning message, got %q", view)
	}
}

func TestRunnerViewPendingSteps(t *testing.T) {
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "done", Run: noop},
			{Title: "todo", Run: noop},
		}},
	}
	m := newTestRunner(phases)
	m.steps[0].status = statusDone
	m.steps[1].status = statusPending
	m.current = 1
	view := m.View()
	if !strings.Contains(view, "○ todo") {
		t.Errorf("view should show pending step with ○, got %q", view)
	}
}

func TestRunPhasesPlain(t *testing.T) {
	var order []string
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "a", Run: func(ctx context.Context, send func(StepEvent)) error {
				order = append(order, "a")
				return nil
			}},
			{Title: "b", Run: func(ctx context.Context, send func(StepEvent)) error {
				order = append(order, "b")
				return nil
			}},
		}},
	}
	err := runPhasesPlain(context.Background(), "Test", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("order = %v, want [a b]", order)
	}
}

func TestRunPhasesPlainError(t *testing.T) {
	testErr := errors.New("fail")
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "ok", Run: noop},
			{Title: "bad", Run: func(ctx context.Context, send func(StepEvent)) error { return testErr }},
			{Title: "skip", Run: noop},
		}},
	}
	err := runPhasesPlain(context.Background(), "Test", phases)
	if !errors.Is(err, testErr) {
		t.Errorf("err = %v, want %v", err, testErr)
	}
}

func TestRunPhasesPlainNonFatal(t *testing.T) {
	var ran bool
	phases := []Phase{
		{Title: "P", Steps: []Step{
			{Title: "warn", Run: func(ctx context.Context, send func(StepEvent)) error {
				return errors.New("not critical")
			}, NonFatal: true},
			{Title: "next", Run: func(ctx context.Context, send func(StepEvent)) error {
				ran = true
				return nil
			}},
		}},
	}
	err := runPhasesPlain(context.Background(), "Test", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("step after non-fatal should have run")
	}
}

func TestRunPhasesFilterEmpty(t *testing.T) {
	phases := []Phase{
		{Title: "Empty", Steps: nil},
		{Title: "HasSteps", Steps: []Step{{Title: "s", Run: noop}}},
	}
	// RunPhases filters empty phases; just verify it doesn't crash.
	err := runPhasesPlain(context.Background(), "Test", phases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 3: Run the tests**

Run: `go test ./internal/tui/... -v`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/tui/runner.go internal/tui/runner_test.go
git commit -m "feat: add phased TUI runner with nested sub-steps and step counter"
```

---

### Task 2: Delete old step runner

**Files:**
- Delete: `internal/tui/step.go`
- Delete: `internal/tui/step_test.go`

**Step 1: Delete old files**

```bash
rm internal/tui/step.go internal/tui/step_test.go
```

**Step 2: Run the tests to confirm nothing breaks**

Run: `go test ./internal/tui/... -v`
Expected: All runner tests pass (the old tests are gone, the new ones cover everything).

Note: `cmd/hop/` will fail to compile at this point because it still references `tui.RunSteps`. That's expected -- we fix it in Tasks 3-6.

**Step 3: Commit**

```bash
git add internal/tui/step.go internal/tui/step_test.go
git commit -m "refactor: remove old step runner (replaced by phased runner)"
```

---

### Task 3: Migrate `hop setup` to phased runner

**Files:**
- Modify: `cmd/hop/setup.go`
- Modify: `internal/setup/bootstrap.go` (change `OnStep` signature)

**Step 1: Update `setup.Options.OnStep` to use `StepEvent`**

In `internal/setup/bootstrap.go`, change the `OnStep` field type and update all callsites inside the package:

Change `Options.OnStep`:
```go
// Before:
OnStep func(msg string)

// After:
OnStep func(msg string)
```

Actually, keep `OnStep func(msg string)` as-is in the setup package. The CLI command will wrap it. This avoids the setup package depending on the tui package. The adapter is simple: `opts.OnStep = func(msg string) { send(tui.StepEvent{Message: msg}) }`.

**Step 2: Rewrite `cmd/hop/setup.go` to use `RunPhases`**

The current setup wraps the entire bootstrap in a single step. We need to break it into granular sub-steps. However, `BootstrapWithClient` has internal steps that report via `OnStep` callback. Rather than refactoring the entire bootstrap function, we keep it as a single TUI step but within a phase, and let the `OnStep` callback feed `StepEvent` messages to the spinner.

```go
func (c *SetupCmd) Run() error {
	opts := setup.Options{
		Name:       c.Name,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// SSH connect with TOFU happens before the TUI (interactive prompt).
	client, capturedKey, err := setup.SSHConnectTOFU(ctx, opts, os.Stdout)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	// Bootstrap via phased TUI runner.
	phases := []tui.Phase{
		{Title: "Bootstrap", Steps: []tui.Step{
			{Title: "Setting up " + c.Name, Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				opts.OnStep = func(msg string) { send(tui.StepEvent{Message: msg}) }
				_, err := setup.BootstrapWithClient(ctx, client, capturedKey, opts, os.Stdout)
				if err != nil {
					return err
				}
				send(tui.StepEvent{Message: fmt.Sprintf("%s ready", c.Name)})
				return nil
			}},
		}},
	}
	if err := tui.RunPhases(ctx, "hop setup", phases); err != nil {
		return err
	}

	// Auto-set as default host if no default is configured yet.
	if gcfg, err := hostconfig.LoadGlobalConfig(); err == nil && gcfg.DefaultHost == "" {
		if err := hostconfig.SetDefaultHost(c.Name); err == nil {
			fmt.Printf("Default host set to %q.\n", c.Name)
		}
	}

	// Install privileged helper if not already present.
	if !helper.IsInstalled() {
		fmt.Println("\nHopbox needs to install a system helper for tunnel networking.")
		fmt.Print("This requires sudo. Proceed? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			helperBin, err := findHelperBinary()
			if err != nil {
				return fmt.Errorf("find helper binary: %w", err)
			}
			cmd := exec.Command("sudo", helperBin, "--install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("install helper: %w", err)
			}
			fmt.Println("Helper installed.")
		} else {
			fmt.Println("Skipped helper installation. hop up will not work without it.")
		}
	}

	return nil
}
```

**Step 3: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: Compiles without errors.

**Step 4: Commit**

```bash
git add cmd/hop/setup.go
git commit -m "refactor: migrate hop setup to phased TUI runner"
```

---

### Task 4: Migrate `hop up` to phased runner

**Files:**
- Modify: `cmd/hop/up.go`

**Step 1: Rewrite the TUI section of `cmd/hop/up.go`**

Replace `[]tui.Step` + `tui.RunSteps` with `[]tui.Phase` + `tui.RunPhases`. The manifest sync and package install steps become `NonFatal: true`.

```go
	// Application setup steps via TUI runner.
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	var phases []tui.Phase

	// Agent phase.
	agentSteps := []tui.Step{
		{Title: fmt.Sprintf("Probing agent at %s", agentURL), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("agent probe failed: %w", err)
			}
			if resp, err := agentClient.Get(agentURL); err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				var health map[string]any
				if json.Unmarshal(body, &health) == nil {
					if agentVer, ok := health["version"].(string); ok && agentVer != version.Version {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf(
							"agent version %q differs from client %q — run 'hop upgrade' to sync",
							agentVer, version.Version)))
					}
				}
			}
			send(tui.StepEvent{Message: "Agent is up"})
			return nil
		}},
	}
	phases = append(phases, tui.Phase{Title: "Agent", Steps: agentSteps})

	// Workspace phase (optional).
	if ws != nil {
		var wsSteps []tui.Step
		wsSteps = append(wsSteps, tui.Step{
			Title: fmt.Sprintf("Syncing manifest: %s", ws.Name),
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				rawManifest, err := os.ReadFile(wsPath)
				if err != nil {
					return err
				}
				if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); err != nil {
					return fmt.Errorf("manifest sync: %w", err)
				}
				send(tui.StepEvent{Message: "Manifest synced"})
				return nil
			},
			NonFatal: true,
		})
		if len(ws.Packages) > 0 {
			wsSteps = append(wsSteps, tui.Step{
				Title: fmt.Sprintf("Installing %d package(s)", len(ws.Packages)),
				Run: func(ctx context.Context, send func(tui.StepEvent)) error {
					pkgs := make([]map[string]string, 0, len(ws.Packages))
					for _, p := range ws.Packages {
						pkgs = append(pkgs, map[string]string{
							"name":    p.Name,
							"backend": p.Backend,
							"version": p.Version,
						})
					}
					if _, err := rpcclient.Call(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
						return fmt.Errorf("package install: %w", err)
					}
					send(tui.StepEvent{Message: "Packages installed"})
					return nil
				},
				NonFatal: true,
			})
		}
		phases = append(phases, tui.Phase{Title: "Workspace", Steps: wsSteps})
	}

	if len(phases) > 0 {
		if err := tui.RunPhases(ctx, "hop up", phases); err != nil {
			return err
		}
	}
```

Note: The manifest sync and package install were previously swallowing errors internally and printing warnings via `fmt.Fprintln(os.Stderr, ui.Warn(...))`. Now they return errors with `NonFatal: true`, which lets the runner display the `⚠` warning properly.

**Step 2: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: Compiles without errors.

**Step 3: Commit**

```bash
git add cmd/hop/up.go
git commit -m "refactor: migrate hop up to phased TUI runner"
```

---

### Task 5: Migrate `hop to` to phased runner

**Files:**
- Modify: `cmd/hop/to.go`

**Step 1: Rewrite `cmd/hop/to.go` to use phases**

Break the 4 steps into 3 phases:

```go
	var snapID string
	var targetCfg *hostconfig.HostConfig

	phases := []tui.Phase{
		{Title: "Snapshot", Steps: []tui.Step{
			{Title: fmt.Sprintf("Creating snapshot on %s", sourceHost), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				snapResult, err := rpcclient.Call(sourceHost, "snap.create", nil)
				if err != nil {
					return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
				}
				var snap struct {
					SnapshotID string `json:"snapshot_id"`
				}
				if err := json.Unmarshal(snapResult, &snap); err != nil || snap.SnapshotID == "" {
					return fmt.Errorf("could not parse snapshot ID from response: %s", string(snapResult))
				}
				snapID = snap.SnapshotID
				send(tui.StepEvent{Message: fmt.Sprintf("Snapshot %s created", snapID)})
				return nil
			}},
		}},
		{Title: "Bootstrap Target", Steps: []tui.Step{
			{Title: fmt.Sprintf("Bootstrapping %s", c.Target), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				bootstrapOpts.OnStep = func(msg string) { send(tui.StepEvent{Message: msg}) }
				var err error
				targetCfg, err = setup.BootstrapWithClient(ctx, sshClient, capturedKey, bootstrapOpts, os.Stdout)
				if err != nil {
					return fmt.Errorf("bootstrap %s: %w", c.Target, err)
				}
				send(tui.StepEvent{Message: fmt.Sprintf("%s bootstrapped", c.Target)})
				return nil
			}},
		}},
		{Title: "Restore", Steps: []tui.Step{
			{Title: fmt.Sprintf("Restoring snapshot on %s", c.Target), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				send(tui.StepEvent{Message: fmt.Sprintf("Connecting to %s", c.Target)})
				tunCfg, err := targetCfg.ToTunnelConfig()
				if err != nil {
					return fmt.Errorf("build tunnel config: %w", err)
				}
				tun := tunnel.NewClientTunnel(tunCfg)

				tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
				defer tunCancel()
				tunErr := make(chan error, 1)
				go func() { tunErr <- tun.Start(tunCtx) }()

				select {
				case <-tun.Ready():
				case err := <-tunErr:
					return fmt.Errorf("tunnel failed to start: %w", err)
				case <-tunCtx.Done():
					return fmt.Errorf("tunnel start timed out")
				}

				agentClient := &http.Client{
					Timeout:   agentClientTimeout,
					Transport: &http.Transport{DialContext: tun.DialContext},
				}
				agentURL := fmt.Sprintf("http://%s:%d/health", targetCfg.AgentIP, tunnel.AgentAPIPort)
				if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
					return fmt.Errorf("target agent unreachable after bootstrap: %w", err)
				}
				send(tui.StepEvent{Message: fmt.Sprintf("Connected to %s", c.Target)})

				send(tui.StepEvent{Message: fmt.Sprintf("Restoring snapshot %s", snapID)})
				if _, err := rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", map[string]string{"id": snapID}); err != nil {
					fmt.Fprintf(os.Stderr, "\nRestore failed. To retry manually:\n")
					fmt.Fprintf(os.Stderr, "  hop snap restore %s --host %s\n", snapID, c.Target)
					return fmt.Errorf("restore on %s: %w", c.Target, err)
				}
				send(tui.StepEvent{Message: fmt.Sprintf("Snapshot restored on %s", c.Target)})
				return nil
			}},
			{Title: fmt.Sprintf("Setting default host to %q", c.Target), Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				if err := hostconfig.SetDefaultHost(c.Target); err != nil {
					return fmt.Errorf("set default host: %w", err)
				}
				send(tui.StepEvent{Message: fmt.Sprintf("Default host set to %q", c.Target)})
				return nil
			}},
		}},
	}

	if err := tui.RunPhases(ctx, "hop to "+c.Target, phases); err != nil {
		return err
	}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: Compiles without errors.

**Step 3: Commit**

```bash
git add cmd/hop/to.go
git commit -m "refactor: migrate hop to to phased TUI runner"
```

---

### Task 6: Migrate `hop upgrade` to phased runner

**Files:**
- Modify: `cmd/hop/upgrade.go`

**Step 1: Rewrite the TUI section of `cmd/hop/upgrade.go`**

Replace `[]tui.Step` + `tui.RunSteps` with `[]tui.Phase` + `tui.RunPhases`. The `sub func(string)` callbacks change to `send func(tui.StepEvent)`.

```go
	// Client + Agent upgrades via TUI step runner.
	var steps []tui.Step
	if doClient {
		tv := targetVersion
		steps = append(steps, tui.Step{
			Title: "Upgrading client",
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				return c.upgradeClientStep(ctx, tv, func(msg string) { send(tui.StepEvent{Message: msg}) })
			},
		})
	}
	if doAgent && sshClient != nil {
		tv := targetVersion
		steps = append(steps, tui.Step{
			Title: "Upgrading agent",
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				return c.upgradeAgentStepWithClient(ctx, sshClient, agentCfg, agentHostName, tv, func(msg string) { send(tui.StepEvent{Message: msg}) })
			},
		})
	} else if doAgent && sshClient == nil && agentHostName == "" {
		steps = append(steps, tui.Step{
			Title: "Upgrading agent",
			Run: func(ctx context.Context, send func(tui.StepEvent)) error {
				send(tui.StepEvent{Message: "Agent: skipped (no host configured)"})
				return nil
			},
		})
	}

	if len(steps) > 0 {
		phases := []tui.Phase{{Title: "Upgrade", Steps: steps}}
		if err := tui.RunPhases(ctx, "hop upgrade", phases); err != nil {
			return err
		}
	}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: Compiles without errors.

**Step 3: Commit**

```bash
git add cmd/hop/upgrade.go
git commit -m "refactor: migrate hop upgrade to phased TUI runner"
```

---

### Task 7: Full build and test verification

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass. No references to the old `tui.RunSteps` or `tui.Step` with `sub func(string)` remain.

**Step 2: Run the linter**

Run: `golangci-lint run`
Expected: No lint errors.

**Step 3: Build all binaries**

Run: `make build`
Expected: All binaries build successfully.

**Step 4: Verify no old references remain**

Run: `grep -r "RunSteps\|sub func(string)" internal/ cmd/`
Expected: No matches.

**Step 5: Commit any cleanup (if needed)**

```bash
git add -A
git commit -m "chore: clean up old step runner references"
```
