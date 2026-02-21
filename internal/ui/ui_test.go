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
