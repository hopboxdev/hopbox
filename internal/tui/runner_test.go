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
