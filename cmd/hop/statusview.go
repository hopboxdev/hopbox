package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Colors.
	green  = lipgloss.Color("2")
	red    = lipgloss.Color("1")
	yellow = lipgloss.Color("3")
	subtle = lipgloss.Color("8")

	// Styles.
	sectionStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtle).
			Padding(0, 1).
			MarginBottom(1)

	titleStyle = lipgloss.NewStyle().
			Bold(true)

	dotConnected    = lipgloss.NewStyle().Foreground(green).Render("●")
	dotDisconnected = lipgloss.NewStyle().Foreground(red).Render("●")
	dotStopped      = lipgloss.NewStyle().Foreground(yellow).Render("●")
)

// renderDashboard renders the full dashboard view from dashData.
func renderDashboard(d dashData, width int) string {
	if width > 80 {
		width = 80
	}
	contentWidth := width - 4 // account for border + padding
	if contentWidth < 40 {
		contentWidth = 40
	}

	var sections []string

	// --- Tunnel section ---
	sections = append(sections, renderTunnelSection(d, contentWidth))

	// --- Services section ---
	if d.tunnelUp && len(d.services) > 0 {
		sections = append(sections, renderServicesSection(d, contentWidth))
	}

	// --- Bridges section ---
	if d.tunnelUp && len(d.bridges) > 0 {
		sections = append(sections, renderBridgesSection(d, contentWidth))
	}

	return strings.Join(sections, "\n")
}

func renderTunnelSection(d dashData, width int) string {
	var lines []string

	status := dotDisconnected + " down"
	if d.tunnelUp && d.connected {
		status = dotConnected + " connected"
	} else if d.tunnelUp {
		status = dotDisconnected + " disconnected"
	}

	lines = append(lines, renderRow("HOST", d.hostName, "STATUS", status, width))
	lines = append(lines, renderRow("ENDPOINT", d.endpoint, "", "", width))

	if d.tunnelUp {
		pingStr := "-"
		if d.ping > 0 {
			pingStr = fmt.Sprintf("%dms", d.ping.Milliseconds())
		}
		lines = append(lines, renderRow("LATENCY", pingStr, "UPTIME", formatDuration(d.uptime), width))

		healthyStr := "-"
		if d.lastHealthy > 0 {
			healthyStr = fmt.Sprintf("%s ago", formatDuration(d.lastHealthy))
		}
		agentStr := d.agentVer
		if agentStr == "" {
			agentStr = "-"
		}
		lines = append(lines, renderRow("LAST HEALTHY", healthyStr, "AGENT", agentStr, width))
	}

	content := strings.Join(lines, "\n")
	return sectionStyle.Width(width).Render(
		titleStyle.Render("Tunnel") + "\n" + content,
	)
}

func renderServicesSection(d dashData, width int) string {
	header := fmt.Sprintf("%-16s %-10s %s", "NAME", "STATUS", "TYPE")
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(subtle).Render(header))

	for _, s := range d.services {
		dot := dotStopped
		status := "stopped"
		if s.Running {
			dot = dotConnected
			status = "running"
		}
		if s.Error != "" {
			dot = dotDisconnected
			status = "error"
		}
		lines = append(lines, fmt.Sprintf("%-16s %s %-8s %s", s.Name, dot, status, s.Type))
	}

	content := strings.Join(lines, "\n")
	return sectionStyle.Width(width).Render(
		titleStyle.Render("Services") + "\n" + content,
	)
}

func renderBridgesSection(d dashData, width int) string {
	var lines []string

	for _, b := range d.bridges {
		dot := dotStopped
		status := "inactive"
		if b.Active {
			dot = dotConnected
			status = "active"
		}
		lines = append(lines, fmt.Sprintf("%-16s %s %s", b.Type, dot, status))
	}

	content := strings.Join(lines, "\n")
	return sectionStyle.Width(width).Render(
		titleStyle.Render("Bridges") + "\n" + content,
	)
}

// renderRow renders a two-column key-value row, with optional second pair.
func renderRow(k1, v1, k2, v2 string, width int) string {
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
