package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestStepRunnerInit(t *testing.T) {
	steps := []Step{
		{Title: "first step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "second step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		spinner: spinner.New(),
	}
	m.Init()
	if m.subMsg != "first step" {
		t.Errorf("Init: subMsg = %q, want %q", m.subMsg, "first step")
	}
	if m.current != 0 {
		t.Errorf("Init: current = %d, want 0", m.current)
	}
}

func TestStepRunnerSubStep(t *testing.T) {
	steps := []Step{
		{Title: "main step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "main step",
		spinner: spinner.New(),
	}
	model, _ := m.Update(subStepMsg{msg: "doing work"})
	r := model.(*stepRunner)
	if r.subMsg != "doing work" {
		t.Errorf("subMsg = %q, want %q", r.subMsg, "doing work")
	}
}

func TestStepRunnerStepDone(t *testing.T) {
	steps := []Step{
		{Title: "step 1", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "step 2", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "step 1",
		spinner: spinner.New(),
	}
	model, _ := m.Update(stepDoneMsg{index: 0, err: nil})
	r := model.(*stepRunner)
	if r.current != 1 {
		t.Errorf("current = %d, want 1", r.current)
	}
	if r.subMsg != "step 2" {
		t.Errorf("subMsg = %q, want %q", r.subMsg, "step 2")
	}
}

func TestStepRunnerLastStepDone(t *testing.T) {
	steps := []Step{
		{Title: "only step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "only step",
		spinner: spinner.New(),
	}
	model, _ := m.Update(stepDoneMsg{index: 0, err: nil})
	r := model.(*stepRunner)
	if r.current != 1 {
		t.Errorf("current = %d, want 1", r.current)
	}
	if r.err != nil {
		t.Errorf("err = %v, want nil", r.err)
	}
}

func TestStepRunnerStepError(t *testing.T) {
	steps := []Step{
		{Title: "failing step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "failing step",
		spinner: spinner.New(),
	}
	testErr := errors.New("something broke")
	model, _ := m.Update(stepDoneMsg{index: 0, err: testErr})
	r := model.(*stepRunner)
	if r.err != testErr {
		t.Errorf("err = %v, want %v", r.err, testErr)
	}
}

func TestStepRunnerViewShowsSpinner(t *testing.T) {
	steps := []Step{
		{Title: "running", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "running",
		spinner: spinner.New(),
	}
	view := m.View()
	if !strings.Contains(view, "running") {
		t.Errorf("View = %q, want to contain %q", view, "running")
	}
}

func TestStepRunnerViewEmptyWhenDone(t *testing.T) {
	steps := []Step{
		{Title: "done", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		current: 1, // past the last step
		spinner: spinner.New(),
	}
	if view := m.View(); view != "" {
		t.Errorf("View = %q, want empty", view)
	}
}

func TestRunStepsPlain(t *testing.T) {
	var order []string
	steps := []Step{
		{Title: "step A", Run: func(ctx context.Context, sub func(string)) error {
			order = append(order, "A")
			return nil
		}},
		{Title: "step B", Run: func(ctx context.Context, sub func(string)) error {
			order = append(order, "B")
			return nil
		}},
	}
	err := runStepsPlain(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Errorf("order = %v, want [A B]", order)
	}
}

func TestRunStepsPlainError(t *testing.T) {
	testErr := errors.New("fail")
	steps := []Step{
		{Title: "ok", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "bad", Run: func(ctx context.Context, sub func(string)) error { return testErr }},
		{Title: "skip", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	err := runStepsPlain(context.Background(), steps)
	if !errors.Is(err, testErr) {
		t.Errorf("err = %v, want %v", err, testErr)
	}
}

func TestRunStepsPlainSubSteps(t *testing.T) {
	steps := []Step{
		{Title: "main", Run: func(ctx context.Context, sub func(string)) error {
			sub("sub-a")
			sub("sub-b")
			return nil
		}},
	}
	err := runStepsPlain(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
