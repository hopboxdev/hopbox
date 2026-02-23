package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MaxWidth is the maximum width for styled output.
const MaxWidth = 80

// Colors.
var (
	Green  = lipgloss.Color("2")
	Red    = lipgloss.Color("1")
	Yellow = lipgloss.Color("3")
	Subtle = lipgloss.Color("8")
)

// DotState represents the state of a status dot.
type DotState int

const (
	StateConnected    DotState = iota // green
	StateDisconnected                 // red
	StateStopped                      // yellow
)

var sectionStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(Subtle).
	Padding(0, 1).
	MarginBottom(1)

var titleStyle = lipgloss.NewStyle().Bold(true)

// Dot returns a colored ● for the given state.
func Dot(state DotState) string {
	switch state {
	case StateConnected:
		return lipgloss.NewStyle().Foreground(Green).Render("●")
	case StateDisconnected:
		return lipgloss.NewStyle().Foreground(Red).Render("●")
	case StateStopped:
		return lipgloss.NewStyle().Foreground(Yellow).Render("●")
	default:
		return "●"
	}
}

// Section renders content inside a bordered box with a bold title.
func Section(title, content string, width int) string {
	if width > MaxWidth {
		width = MaxWidth
	}
	contentWidth := max(width-4, 40)
	return sectionStyle.Width(contentWidth).Render(
		titleStyle.Render(title) + "\n" + content,
	)
}

// StepOK returns a green checkmark step line.
func StepOK(msg string) string {
	return lipgloss.NewStyle().Foreground(Green).Render("✔") + " " + msg
}

// StepRun returns a yellow circle step line (in progress).
func StepRun(msg string) string {
	return lipgloss.NewStyle().Foreground(Yellow).Render("○") + " " + msg
}

// StepInfo returns a subtle-colored info step line.
func StepInfo(msg string) string {
	return lipgloss.NewStyle().Foreground(Subtle).Render("●") + " " + msg
}

// StepFail returns a red cross step line.
func StepFail(msg string) string {
	return lipgloss.NewStyle().Foreground(Red).Render("✘") + " " + msg
}

// Warn returns a yellow warning message (caller writes to stderr).
func Warn(msg string) string {
	return lipgloss.NewStyle().Foreground(Yellow).Render("⚠") + " " + msg
}

// Error returns a red error message (caller writes to stderr).
func Error(msg string) string {
	return lipgloss.NewStyle().Foreground(Red).Render("✘") + " " + msg
}

// Table renders columnar data with subtle-colored headers.
// Each row is a slice of strings matching the headers length.
func Table(headers []string, rows [][]string) string {
	// Calculate column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Render header.
	var headerParts []string
	for i, h := range headers {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", widths[i], h))
	}
	headerLine := lipgloss.NewStyle().Foreground(Subtle).Render(
		strings.Join(headerParts, "  "),
	)

	// Render rows.
	var lines []string
	lines = append(lines, headerLine)
	for _, row := range rows {
		var parts []string
		for i, cell := range row {
			w := 0
			if i < len(widths) {
				w = widths[i]
			}
			parts = append(parts, fmt.Sprintf("%-*s", w, cell))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	return strings.Join(lines, "\n")
}

// Row renders a two-column key-value row, with optional second pair.
func Row(k1, v1, k2, v2 string, width int) string {
	left := fmt.Sprintf("%-14s %s", k1+":", v1)
	if k2 == "" {
		return left
	}
	right := fmt.Sprintf("%s %s", k2+":", v2)
	gap := width/2 - len(left)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}
