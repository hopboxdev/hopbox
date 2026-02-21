# TUI Status Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the static `hop status` output with a live-updating Bubble Tea TUI showing tunnel health, services, and bridges.

**Architecture:** A Bubble Tea program in `cmd/hop/status.go` that polls data from the tunnel state file, agent `/health` endpoint, `services.list` RPC, and the workspace manifest for bridges. Data is refreshed every 5 seconds via a tick command. Styling uses lipgloss for bordered sections.

**Tech Stack:** Go, charmbracelet/bubbletea, charmbracelet/lipgloss

---

### Task 1: Add Bubble Tea and Lipgloss dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add dependencies**

Run: `go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss`
Expected: Both added to go.mod

**Step 2: Tidy**

Run: `go mod tidy`
Expected: go.sum updated, no errors

**Step 3: Verify build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add bubbletea and lipgloss dependencies"
```

---

### Task 2: Create the dashboard data model

**Files:**
- Create: `cmd/hop/statusmodel.go`
- Create: `cmd/hop/statusmodel_test.go`

This file defines the data structures and the data-fetching function that the TUI will use.

**Step 1: Write the failing test**

Create `cmd/hop/statusmodel_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{3661 * time.Second, "1h 1m"},
		{86400 * time.Second, "1d 0h"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/hop/... -run "TestFormatBytes|TestFormatDuration" -v`
Expected: FAIL — functions not defined

**Step 3: Implement the data model**

Create `cmd/hop/statusmodel.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// dashData holds all data displayed by the status TUI.
type dashData struct {
	// Host info
	hostName string
	endpoint string

	// Tunnel
	tunnelUp    bool
	connected   bool
	uptime      time.Duration
	lastHealthy time.Duration
	ping        time.Duration
	agentVer    string

	// Services
	services []svcInfo

	// Bridges (from manifest)
	bridges []bridgeInfo

	// Error from last fetch
	err error
}

type svcInfo struct {
	Name    string
	Type    string
	Running bool
	Error   string
}

type bridgeInfo struct {
	Type   string
	Active bool
}

// fetchDashData collects all dashboard data from local state + agent API.
func fetchDashData(hostName string, cfg *hostconfig.HostConfig) dashData {
	d := dashData{
		hostName: hostName,
		endpoint: cfg.Endpoint,
	}

	// 1. Tunnel state (local file).
	state, _ := tunnel.LoadState(hostName)
	if state == nil {
		return d
	}
	d.tunnelUp = true
	d.connected = state.Connected
	d.uptime = time.Since(state.StartedAt)
	if !state.LastHealthy.IsZero() {
		d.lastHealthy = time.Since(state.LastHealthy)
	}

	// 2. Health ping (measures round-trip).
	hostname := state.Hostname
	if hostname == "" {
		hostname = hostName + ".hop"
	}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)
	client := &http.Client{Timeout: 5 * time.Second}

	start := time.Now()
	resp, err := client.Get(agentURL)
	if err == nil {
		d.ping = time.Since(start)
		defer func() { _ = resp.Body.Close() }()

		var health map[string]any
		body, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(body, &health)
		if v, ok := health["version"]; ok {
			d.agentVer = fmt.Sprint(v)
		}
	}

	// 3. Services (RPC).
	svcResult, err := rpcclient.Call(hostName, "services.list", nil)
	if err == nil {
		var svcs []svcInfo
		if json.Unmarshal(svcResult, &svcs) == nil {
			d.services = svcs
		}
	}

	// 4. Bridges (from manifest — assumes active if tunnel is up).
	ws, err := manifest.Parse("hopbox.yaml")
	if err == nil {
		for _, b := range ws.Bridges {
			d.bridges = append(d.bridges, bridgeInfo{
				Type:   b.Type,
				Active: d.connected,
			})
		}
	}

	return d
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatDuration formats a duration as a compact human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, h)
}
```

**Step 4: Run tests**

Run: `go test ./cmd/hop/... -run "TestFormatBytes|TestFormatDuration" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/hop/statusmodel.go cmd/hop/statusmodel_test.go
git commit -m "feat: add TUI dashboard data model and formatting helpers"
```

---

### Task 3: Build the TUI view

**Files:**
- Create: `cmd/hop/statusview.go`

This file contains the lipgloss styling and render function that turns `dashData` into a string.

**Step 1: Implement the view**

Create `cmd/hop/statusview.go`:

```go
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

	footerStyle = lipgloss.NewStyle().
			Foreground(subtle).
			Align(lipgloss.Right)
)

// renderDashboard renders the full dashboard view from dashData.
func renderDashboard(d dashData, width int) string {
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

	// --- Footer ---
	footer := footerStyle.Width(width).Render("q quit · r refresh")
	sections = append(sections, footer)

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
		lines = append(lines, renderRow("PING", pingStr, "UPTIME", formatDuration(d.uptime), width))

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
	half := width / 2
	left := fmt.Sprintf("%-14s %s", k1+":", v1)
	if k2 == "" {
		return left
	}
	right := fmt.Sprintf("%-14s %s", k2+":", v2)
	gap := half - len(left)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}
```

**Step 2: Verify build**

Run: `go build ./cmd/hop/...`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/hop/statusview.go
git commit -m "feat: add TUI dashboard view with lipgloss styling"
```

---

### Task 4: Wire up the Bubble Tea program

**Files:**
- Rewrite: `cmd/hop/status.go`

Replace the static `StatusCmd` with a Bubble Tea program that uses the data model and view from Tasks 2-3.

**Step 1: Rewrite status.go**

Replace the entire file with:

```go
package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// StatusCmd shows tunnel and workspace health.
type StatusCmd struct{}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	p := tea.NewProgram(newDashModel(hostName, cfg))
	_, err = p.Run()
	return err
}

// dashModel is the Bubble Tea model for the status dashboard.
type dashModel struct {
	hostName string
	cfg      *hostconfig.HostConfig
	data     dashData
	width    int
	quitting bool
}

func newDashModel(hostName string, cfg *hostconfig.HostConfig) dashModel {
	return dashModel{
		hostName: hostName,
		cfg:      cfg,
		data:     fetchDashData(hostName, cfg),
		width:    80,
	}
}

// Messages.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type refreshMsg struct {
	data dashData
}

func (m dashModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		return refreshMsg{data: fetchDashData(m.hostName, m.cfg)}
	}
}

func (m dashModel) Init() tea.Cmd {
	return tickCmd()
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.fetchCmd()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.fetchCmd(), tickCmd())

	case refreshMsg:
		m.data = msg.data
		return m, nil
	}

	return m, nil
}

func (m dashModel) View() string {
	if m.quitting {
		return ""
	}
	return renderDashboard(m.data, m.width)
}
```

**Step 2: Remove unused imports check**

Run: `go build ./cmd/hop/...`
Expected: PASS — no unused imports, compiles cleanly

**Step 3: Run all tests**

Run: `go test ./...`
Expected: All PASS

**Step 4: Build and manual test**

Run: `make build`
Expected: All three binaries build successfully

Run: `./dist/hop status`
Expected: TUI renders with tunnel info, services, bridges. Press `q` to quit.

**Step 5: Commit**

```bash
git add cmd/hop/status.go
git commit -m "feat: replace static hop status with live Bubble Tea TUI"
```

---

### Manual Testing

After all tasks are complete, verify the TUI:

```bash
# 1. Build
make build

# 2. With tunnel running (hop up in another terminal)
./dist/hop status
# Should show: Tunnel connected, ping, services, bridges
# Should auto-refresh every 5s
# Press r to force refresh, q to quit

# 3. Without tunnel running
hop down
./dist/hop status
# Should show: Tunnel down, no services/bridges sections
# Press q to quit

# 4. Verify terminal resize
# Drag terminal window — layout should adapt to width
```
