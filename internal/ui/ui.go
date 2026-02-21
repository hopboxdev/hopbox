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
	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}
	return sectionStyle.Width(contentWidth).Render(
		titleStyle.Render(title) + "\n" + content,
	)
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
