package ui

import (
	"strings"
	"testing"
)

func TestDot(t *testing.T) {
	tests := []struct {
		state DotState
		want  string
	}{
		{StateConnected, "●"},
		{StateDisconnected, "●"},
		{StateStopped, "●"},
	}
	for _, tt := range tests {
		got := Dot(tt.state)
		if !strings.Contains(got, tt.want) {
			t.Errorf("Dot(%v) = %q, want to contain %q", tt.state, got, tt.want)
		}
	}
}

func TestSection(t *testing.T) {
	out := Section("Test", "hello", 40)
	if !strings.Contains(out, "Test") {
		t.Error("Section missing title")
	}
	if !strings.Contains(out, "hello") {
		t.Error("Section missing content")
	}
	// Rounded border characters
	if !strings.Contains(out, "╭") {
		t.Error("Section missing rounded border")
	}
}

func TestRow(t *testing.T) {
	got := Row("KEY1", "val1", "KEY2", "val2", 60)
	if !strings.Contains(got, "KEY1:") || !strings.Contains(got, "val1") {
		t.Error("Row missing left pair")
	}
	if !strings.Contains(got, "KEY2:") || !strings.Contains(got, "val2") {
		t.Error("Row missing right pair")
	}
}

func TestRowSinglePair(t *testing.T) {
	got := Row("KEY", "value", "", "", 60)
	if !strings.Contains(got, "KEY:") {
		t.Error("Row missing key")
	}
}

func TestStepOK(t *testing.T) {
	got := StepOK("tunnel established")
	if !strings.Contains(got, "✔") {
		t.Error("StepOK missing checkmark")
	}
	if !strings.Contains(got, "tunnel established") {
		t.Error("StepOK missing message")
	}
}

func TestStepRun(t *testing.T) {
	got := StepRun("starting services")
	if !strings.Contains(got, "○") {
		t.Error("StepRun missing circle")
	}
}

func TestStepFail(t *testing.T) {
	got := StepFail("connection refused")
	if !strings.Contains(got, "✘") {
		t.Error("StepFail missing cross")
	}
}

func TestWarn(t *testing.T) {
	got := Warn("something happened")
	if !strings.Contains(got, "⚠") {
		t.Error("Warn missing warning symbol")
	}
	if !strings.Contains(got, "something happened") {
		t.Error("Warn missing message")
	}
}

func TestError(t *testing.T) {
	got := Error("bad thing")
	if !strings.Contains(got, "✘") {
		t.Error("Error missing cross")
	}
}

func TestTable(t *testing.T) {
	headers := []string{"NAME", "TYPE", "STATUS"}
	rows := [][]string{
		{"postgres", "docker", "running"},
		{"redis", "docker", "stopped"},
	}
	got := Table(headers, rows)
	if !strings.Contains(got, "NAME") {
		t.Error("Table missing header")
	}
	if !strings.Contains(got, "postgres") {
		t.Error("Table missing row data")
	}
}
