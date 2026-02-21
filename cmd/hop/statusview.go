package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hopboxdev/hopbox/internal/ui"
)

// renderDashboard renders the full dashboard view from dashData.
func renderDashboard(d dashData, width int) string {
	if width > ui.MaxWidth {
		width = ui.MaxWidth
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

	status := ui.Dot(ui.StateDisconnected) + " down"
	if d.tunnelUp && d.connected {
		status = ui.Dot(ui.StateConnected) + " connected"
	} else if d.tunnelUp {
		status = ui.Dot(ui.StateDisconnected) + " disconnected"
	}

	lines = append(lines, ui.Row("HOST", d.hostName, "STATUS", status, width))
	lines = append(lines, ui.Row("ENDPOINT", d.endpoint, "", "", width))

	if d.tunnelUp {
		pingStr := "-"
		if d.ping > 0 {
			pingStr = fmt.Sprintf("%dms", d.ping.Milliseconds())
		}
		lines = append(lines, ui.Row("LATENCY", pingStr, "UPTIME", formatDuration(d.uptime), width))

		healthyStr := "-"
		if d.lastHealthy > 0 {
			healthyStr = fmt.Sprintf("%s ago", formatDuration(d.lastHealthy))
		}
		agentStr := d.agentVer
		if agentStr == "" {
			agentStr = "-"
		}
		lines = append(lines, ui.Row("LAST HEALTHY", healthyStr, "AGENT", agentStr, width))
	}

	content := strings.Join(lines, "\n")
	return ui.Section("Tunnel", content, width)
}

func renderServicesSection(d dashData, width int) string {
	header := fmt.Sprintf("%-16s %-10s %s", "NAME", "STATUS", "TYPE")
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(ui.Subtle).Render(header))

	for _, s := range d.services {
		dot := ui.Dot(ui.StateStopped)
		status := "stopped"
		if s.Running {
			dot = ui.Dot(ui.StateConnected)
			status = "running"
		}
		if s.Error != "" {
			dot = ui.Dot(ui.StateDisconnected)
			status = "error"
		}
		lines = append(lines, fmt.Sprintf("%-16s %s %-8s %s", s.Name, dot, status, s.Type))
	}

	content := strings.Join(lines, "\n")
	return ui.Section("Services", content, width)
}

func renderBridgesSection(d dashData, width int) string {
	var lines []string

	for _, b := range d.bridges {
		dot := ui.Dot(ui.StateStopped)
		status := "inactive"
		if b.Active {
			dot = ui.Dot(ui.StateConnected)
			status = "active"
		}
		lines = append(lines, fmt.Sprintf("%-16s %s %s", b.Type, dot, status))
	}

	content := strings.Join(lines, "\n")
	return ui.Section("Bridges", content, width)
}
