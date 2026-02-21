# CLI Styling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract styling into a shared `internal/ui` package and apply consistent styled output across all CLI commands.

**Architecture:** Create `internal/ui` with lipgloss primitives (Section, Dot, Row, Table, StepOK/Run/Fail, Warn, Error). Refactor `statusview.go` to consume it. Update list commands (services/bridge/host/snap ls) to use bordered sections with colored dots. Update lifecycle commands (up/setup/to) to use styled steps. Update remaining commands (upgrade/rotate) with styled warnings.

**Tech Stack:** `github.com/charmbracelet/lipgloss` (already a dependency)

---

### Task 1: Create `internal/ui` package — core types and Section/Dot/Row

**Files:**
- Create: `internal/ui/ui.go`
- Create: `internal/ui/ui_test.go`

**Step 1: Write failing tests for Dot and Section**

```go
// internal/ui/ui_test.go
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
	if strings.Contains(got, ":  ") {
		// Should not have trailing empty pair
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/... -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Implement core primitives**

```go
// internal/ui/ui.go
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/ui.go internal/ui/ui_test.go
git commit -m "feat: add internal/ui package with Section, Dot, Row primitives"
```

---

### Task 2: Add step and message primitives to `internal/ui`

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_test.go`

**Step 1: Write failing tests for StepOK, StepRun, StepFail, Warn, Error, Table**

Add to `internal/ui/ui_test.go`:

```go
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
```

**Step 2: Run tests to verify new tests fail**

Run: `go test ./internal/ui/... -v -run "TestStep|TestWarn|TestError|TestTable"`
Expected: FAIL — functions not defined

**Step 3: Implement step, message, and table primitives**

Add to `internal/ui/ui.go`:

```go
// StepOK returns a green checkmark step line.
func StepOK(msg string) string {
	return lipgloss.NewStyle().Foreground(Green).Render("✔") + " " + msg
}

// StepRun returns a yellow circle step line (in progress).
func StepRun(msg string) string {
	return lipgloss.NewStyle().Foreground(Yellow).Render("○") + " " + msg
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
```

**Step 4: Run all ui tests**

Run: `go test ./internal/ui/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/ui.go internal/ui/ui_test.go
git commit -m "feat: add Step, Warn, Error, Table primitives to internal/ui"
```

---

### Task 3: Refactor `statusview.go` to use `internal/ui`

**Files:**
- Modify: `cmd/hop/statusview.go`

This is a pure extraction — no visual changes. Replace local color vars, styles, and dot renderings with `ui.*` calls.

**Step 1: Run existing tests to establish baseline**

Run: `go test ./cmd/hop/... -v -run "TestFormat"`
Expected: PASS (formatBytes, formatDuration tests pass)

**Step 2: Refactor statusview.go**

Replace the entire `statusview.go` contents. Key changes:
- Delete `green`, `red`, `yellow`, `subtle` color vars
- Delete `sectionStyle`, `titleStyle` vars
- Delete `dotConnected`, `dotDisconnected`, `dotStopped` vars
- Delete `renderRow` function (moved to `ui.Row`)
- Import `github.com/hopboxdev/hopbox/internal/ui`
- `renderDashboard` uses `ui.MaxWidth` instead of hardcoded 80
- `renderTunnelSection` uses `ui.Section`, `ui.Dot`, `ui.Row`
- `renderServicesSection` uses `ui.Section`, `ui.Dot`, `ui.Table` or keeps inline formatting with `ui.Dot`
- `renderBridgesSection` uses `ui.Section`, `ui.Dot`

The refactored file should look like:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/hopboxdev/hopbox/internal/ui"
)

func renderDashboard(d dashData, width int) string {
	if width > ui.MaxWidth {
		width = ui.MaxWidth
	}
	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	var sections []string
	sections = append(sections, renderTunnelSection(d, contentWidth))

	if d.tunnelUp && len(d.services) > 0 {
		sections = append(sections, renderServicesSection(d, contentWidth))
	}
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
```

Note: `renderServicesSection` and `renderBridgesSection` keep their inline formatting with `ui.Dot` calls (matching the status dashboard style which interleaves dots with columns). The `ui.Table` primitive is for the standalone list commands.

**Step 3: Fix imports — statusview.go still needs lipgloss for Subtle in services header**

The services section header uses `lipgloss.NewStyle().Foreground(ui.Subtle)` directly. This is fine — `ui.Subtle` is the exported color, lipgloss is still needed for the inline rendering.

**Step 4: Run tests**

Run: `go test ./cmd/hop/... -v -run "TestFormat"` and `go test ./internal/ui/... -v`
Expected: PASS

**Step 5: Run full build**

Run: `go build ./cmd/hop/...`
Expected: Compiles without errors

**Step 6: Commit**

```bash
git add cmd/hop/statusview.go
git commit -m "refactor: extract statusview styles into internal/ui package"
```

---

### Task 4: Style `hop services ls`

**Files:**
- Modify: `cmd/hop/services.go:22-57` (ServicesLsCmd.Run)

**Step 1: Replace tabwriter with ui.Section + ui.Dot**

Replace the `ServicesLsCmd.Run` method:

```go
func (c *ServicesLsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	result, err := rpcclient.Call(hostName, "services.list", nil)
	if err != nil {
		return err
	}
	var svcs []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Running bool   `json:"running"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(result, &svcs); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(svcs) == 0 {
		fmt.Println(ui.Section("Services", "No services.", ui.MaxWidth))
		return nil
	}

	headers := []string{"NAME", "STATUS", "TYPE"}
	var rows [][]string
	for _, s := range svcs {
		dot := ui.Dot(ui.StateStopped)
		status := "stopped"
		if s.Running {
			dot = ui.Dot(ui.StateConnected)
			status = "running"
		}
		if s.Error != "" {
			dot = ui.Dot(ui.StateDisconnected)
			status = "error: " + s.Error
		}
		rows = append(rows, []string{dot + " " + s.Name, status, s.Type})
	}
	fmt.Println(ui.Section("Services", ui.Table(headers, rows), ui.MaxWidth))
	return nil
}
```

Update imports: remove `"os"`, `"text/tabwriter"`, add `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/services.go
git commit -m "style: use bordered section for hop services ls"
```

---

### Task 5: Style `hop bridge ls`

**Files:**
- Modify: `cmd/hop/bridge.go:22-37` (BridgeLsCmd.Run)

**Step 1: Replace tabwriter with ui.Section + ui.Dot**

```go
func (c *BridgeLsCmd) Run() error {
	ws, err := manifest.Parse(c.Workspace)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if len(ws.Bridges) == 0 {
		fmt.Println(ui.Section("Bridges", "No bridges configured.", ui.MaxWidth))
		return nil
	}
	var lines []string
	for _, b := range ws.Bridges {
		lines = append(lines, fmt.Sprintf("%s %s   configured", ui.Dot(ui.StateConnected), b.Type))
	}
	fmt.Println(ui.Section("Bridges", strings.Join(lines, "\n"), ui.MaxWidth))
	return nil
}
```

Update imports: remove `"os"`, `"text/tabwriter"`, add `"strings"`, `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/bridge.go
git commit -m "style: use bordered section for hop bridge ls"
```

---

### Task 6: Style `hop host ls`

**Files:**
- Modify: `cmd/hop/host.go:47-65` (HostLsCmd.Run)

**Step 1: Replace plain text with ui.Section + ui.Dot**

```go
func (c *HostLsCmd) Run() error {
	names, err := hostconfig.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println(ui.Section("Hosts", "No hosts configured. Use 'hop setup' to add one.", ui.MaxWidth))
		return nil
	}
	cfg, _ := hostconfig.LoadGlobalConfig()
	var lines []string
	for _, n := range names {
		if cfg != nil && n == cfg.DefaultHost {
			lines = append(lines, fmt.Sprintf("%s %s   (default)", ui.Dot(ui.StateConnected), n))
		} else {
			lines = append(lines, "  " + n)
		}
	}
	fmt.Println(ui.Section("Hosts", strings.Join(lines, "\n"), ui.MaxWidth))
	return nil
}
```

Update imports: add `"strings"`, `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/host.go
git commit -m "style: use bordered section for hop host ls"
```

---

### Task 7: Style `hop snap ls`

**Files:**
- Modify: `cmd/hop/snap.go:60-67` (SnapLsCmd.Run)

Currently uses `rpcclient.CallAndPrint` which dumps raw JSON. Parse the response and render with `ui.Section` + `ui.Table`.

The agent returns `[]snapshot.Info` with fields: `id`, `short_id`, `time`, `paths`, `hostname`, `tags`.

**Step 1: Replace CallAndPrint with parsed + styled output**

```go
func (c *SnapLsCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}
	result, err := rpcclient.Call(hostName, "snap.list", nil)
	if err != nil {
		return err
	}
	var snaps []struct {
		ShortID string    `json:"short_id"`
		Time    time.Time `json:"time"`
	}
	if err := json.Unmarshal(result, &snaps); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(snaps) == 0 {
		fmt.Println(ui.Section("Snapshots", "No snapshots.", ui.MaxWidth))
		return nil
	}

	headers := []string{"ID", "CREATED"}
	var rows [][]string
	for _, s := range snaps {
		age := formatDuration(time.Since(s.Time)) + " ago"
		rows = append(rows, []string{s.ShortID, age})
	}
	fmt.Println(ui.Section("Snapshots", ui.Table(headers, rows), ui.MaxWidth))
	return nil
}
```

Update imports: add `"time"`, `"github.com/hopboxdev/hopbox/internal/ui"`.

Note: `formatDuration` is in `statusmodel.go` in the same `main` package, so it's available directly.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/snap.go
git commit -m "style: use bordered section for hop snap ls"
```

---

### Task 8: Style `hop up`

**Files:**
- Modify: `cmd/hop/up.go`

Replace `fmt.Printf`/`Println` progress messages with `ui.StepOK`/`ui.StepRun`. Replace `fmt.Fprintf(os.Stderr, "Warning: ...")` with `ui.Warn`. Reconnection monitor messages keep their timestamped format.

**Step 1: Replace progress lines**

Key replacements (line numbers reference current file):

| Line | Current | New |
|------|---------|-----|
| 74 | `fmt.Printf("Bringing up tunnel to %s (%s)...\n", ...)` | `fmt.Println(ui.StepRun(fmt.Sprintf("Bringing up tunnel to %s (%s)", ...)))` |
| 107 | `fmt.Printf("Interface %s up, ...\n", ...)` | `fmt.Println(ui.StepOK(fmt.Sprintf("Interface %s up, ...", ...)))` |
| 120 | `fmt.Printf("Loaded workspace: %s\n", ...)` | `fmt.Println(ui.StepOK(fmt.Sprintf("Loaded workspace: %s", ...)))` |
| 133,142 | `fmt.Fprintf(os.Stderr, "... bridge error: %v\n", err)` | `fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("... bridge error: %v", err)))` |
| 153 | `fmt.Printf("Probing agent at %s...\n", ...)` | `fmt.Println(ui.StepRun(fmt.Sprintf("Probing agent at %s", ...)))` |
| 156 | `fmt.Fprintf(os.Stderr, "Warning: agent probe failed: %v\n", err)` | `fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("agent probe failed: %v", err)))` |
| 158 | `fmt.Println("Agent is up.")` | `fmt.Println(ui.StepOK("Agent is up"))` |
| 175,173 | `fmt.Fprintf(os.Stderr, "Warning: ...")` | `fmt.Fprintln(os.Stderr, ui.Warn(...))` |
| 176 | `fmt.Println("Agent upgraded and reachable.")` | `fmt.Println(ui.StepOK("Agent upgraded and reachable"))` |
| 189,192 | Warning / success for manifest sync | `ui.Warn` / `ui.StepOK` |
| 198,206,209 | Package install progress/warnings | `ui.StepRun` / `ui.Warn` / `ui.StepOK` |
| 224 | Warning for tunnel state write | `ui.Warn` |
| 234 | `fmt.Println("Tunnel up. Press Ctrl-C to stop.")` | `fmt.Println(ui.StepOK("Tunnel up. Press Ctrl-C to stop"))` |
| 267 | `fmt.Println("\nShutting down...")` | `fmt.Println("\n" + ui.StepRun("Shutting down..."))` |

Reconnection monitor callbacks (lines 241-254) keep their `fmt.Printf` with timestamps — these are runtime events, not setup steps.

Update imports: add `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/up.go
git commit -m "style: use styled steps and warnings in hop up"
```

---

### Task 9: Style `hop setup` — add OnStep to Bootstrap

**Files:**
- Modify: `internal/setup/bootstrap.go:32-43` (Options struct) and `53-157` (Bootstrap func)
- Modify: `cmd/hop/setup.go`
- Modify: `cmd/hop/to.go` (uses Bootstrap too)

**Step 1: Add OnStep callback to Options**

In `internal/setup/bootstrap.go`, add to the `Options` struct:

```go
// OnStep is called with a progress message at each bootstrap milestone.
// If nil, messages are written to the out io.Writer passed to Bootstrap.
OnStep func(msg string)
```

**Step 2: Update Bootstrap to use OnStep**

Replace the `logf` function in Bootstrap (line 61-63):

```go
logf := func(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if opts.OnStep != nil {
		opts.OnStep(msg)
	} else {
		_, _ = fmt.Fprintln(out, msg)
	}
}
```

This is backward compatible — existing callers that don't set OnStep get the same behavior. The TOFU prompt (lines 78-80) continues to write directly to `out`.

**Step 3: Update `setup.go` to use OnStep with ui.StepOK**

```go
func (c *SetupCmd) Run() error {
	opts := setup.Options{
		Name:       c.Name,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
		OnStep: func(msg string) {
			fmt.Println(ui.StepOK(msg))
		},
	}
	// ... rest unchanged
```

Update imports for setup.go: add `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 4: Update `to.go` to use OnStep with ui.StepOK**

In `to.go` line 71, add OnStep to the Options:

```go
targetCfg, err := setup.Bootstrap(ctx, setup.Options{
	Name:       c.Target,
	SSHHost:    c.Addr,
	SSHPort:    c.Port,
	SSHUser:    c.User,
	SSHKeyPath: c.SSHKey,
	OnStep: func(msg string) {
		fmt.Println("  " + ui.StepOK(msg))
	},
}, os.Stdout)
```

Note the `"  "` indent — `to.go` indents sub-steps under the numbered step headers.

**Step 5: Run existing bootstrap test**

Run: `go test ./internal/setup/... -v -run TestBootstrap`
Expected: PASS (test doesn't set OnStep, so falls back to io.Writer)

**Step 6: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 7: Commit**

```bash
git add internal/setup/bootstrap.go cmd/hop/setup.go cmd/hop/to.go
git commit -m "feat: add OnStep callback to Bootstrap, style hop setup output"
```

---

### Task 10: Style `hop to`

**Files:**
- Modify: `cmd/hop/to.go`

The numbered step format maps to bold step headers with indented `ui.StepOK` lines. OnStep was already added in Task 9.

**Step 1: Style the step headers and output lines**

Key replacements:

```go
// Step 1/4 header (line 56)
// Before: fmt.Printf("\nStep 1/4  Snapshot  creating snapshot on %s...\n", sourceHost)
// After:  fmt.Printf("\nStep 1/4  Snapshot\n")
//         fmt.Println("  " + ui.StepRun(fmt.Sprintf("creating snapshot on %s", sourceHost)))

// Step 1/4 result (line 67)
// Before: fmt.Printf("            snapshot %s created.\n", snap.SnapshotID)
// After:  fmt.Println("  " + ui.StepOK(fmt.Sprintf("snapshot %s created", snap.SnapshotID)))

// Step 2/4 header (line 70)
// Before: fmt.Printf("\nStep 2/4  Bootstrap  setting up %s...\n", c.Target)
// After:  fmt.Printf("\nStep 2/4  Bootstrap\n")
//         fmt.Println("  " + ui.StepRun(fmt.Sprintf("setting up %s", c.Target)))

// Step 2/4 result (line 81)
// Before: fmt.Printf("            %s bootstrapped.\n", c.Target)
// After:  fmt.Println("  " + ui.StepOK(fmt.Sprintf("%s bootstrapped", c.Target)))

// Step 3/4 (lines 84, 114, 120)
// Same pattern — StepRun for "connecting...", StepOK for "snapshot restored"

// Step 4/4 (line 123)
// Before: fmt.Printf("\nStep 4/4  Switch     setting default host to %s...\n", c.Target)
// After:  fmt.Printf("\nStep 4/4  Switch\n")
//         fmt.Println("  " + ui.StepOK(fmt.Sprintf("default host set to %q", c.Target)))

// Confirmation prompt (line 43-48) — keep as plain text, it's interactive

// Final message (lines 128-129)
// Before: fmt.Printf("\nMigration complete. Default host set to %q.\n", c.Target)
// After:  fmt.Println("\n" + ui.StepOK(fmt.Sprintf("Migration complete. Default host set to %q", c.Target)))

// Error recovery hint (lines 116-117) — keep as plain stderr
```

Update imports: add `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 2: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 3: Commit**

```bash
git add cmd/hop/to.go
git commit -m "style: use styled steps in hop to"
```

---

### Task 11: Style `hop upgrade` and `hop rotate`

**Files:**
- Modify: `cmd/hop/upgrade.go`
- Modify: `cmd/hop/rotate.go`

**Step 1: Style upgrade.go**

Key replacements in `upgrade.go`:

```go
// Line 57: fmt.Println("Checking for latest release...")
//       →  fmt.Println(ui.StepRun("Checking for latest release"))

// Line 63: fmt.Printf("Latest release: %s\n\n", targetVersion)
//       →  fmt.Println(ui.StepOK(fmt.Sprintf("Latest release: %s", targetVersion)))

// Line 67: fmt.Println("Upgrading from local builds (./dist/)...")
//       →  fmt.Println(ui.StepRun("Upgrading from local builds (./dist/)"))

// Line 91: fmt.Println("\nUpgrade complete.")
//       →  fmt.Println("\n" + ui.StepOK("Upgrade complete"))

// Line 107-108: fmt.Printf("  Client: installed via %s ...\n", pm)
//            →  fmt.Println(ui.StepOK(fmt.Sprintf("Client: installed via %s — run your package manager to update", pm)))

// Line 113-114: fmt.Printf("  Client: already at %s\n", ...)
//            →  fmt.Println(ui.StepOK(fmt.Sprintf("Client: already at %s", ...)))

// Line 123: fmt.Printf("  Client: upgrading from local build...")
//        →  fmt.Println(ui.StepRun("Client: upgrading from local build"))
// Line 127: fmt.Printf(" done (%s)\n", execPath)
//        →  fmt.Println(ui.StepOK(fmt.Sprintf("Client: upgraded (%s)", execPath)))

// Line 133-134: fmt.Printf("  Client: %s → %s ", version.Version, targetVersion)
//            →  fmt.Println(ui.StepRun(fmt.Sprintf("Client: %s → %s", version.Version, targetVersion)))
// Line 144: fmt.Printf(" done (%s)\n", execPath)
//        →  fmt.Println(ui.StepOK(fmt.Sprintf("Client: upgraded (%s)", execPath)))

// Line 154-155: fmt.Printf("  Helper: already at %s\n", hv)
//            →  fmt.Println(ui.StepOK(fmt.Sprintf("Helper: already at %s", hv)))

// Line 167,171: helper upgrade messages → ui.StepRun
// Line 206: fmt.Println("  Helper: done") → fmt.Println(ui.StepOK("Helper: upgraded"))

// Line 213-214: fmt.Printf("  Agent: skipped ...\n")
//            →  fmt.Println(ui.StepOK("Agent: skipped (no host configured)"))

// Line 224: fmt.Fprintf(os.Stderr, "  Warning: tunnel is running...\n", ...)
//        →  fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("tunnel is running (PID %d). The agent will restart", state.PID)))

// Line 241: fmt.Printf("  Agent (%s): upgrading...\n", hostName)
//        →  fmt.Println(ui.StepRun(fmt.Sprintf("Agent (%s): upgrading", hostName)))
```

**Step 2: Style rotate.go**

```go
// Line 29: fmt.Fprintf(os.Stderr, "Warning: tunnel is running ...\n", state.PID)
//       →  fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("tunnel is running (PID %d). Run 'hop down && hop up' after rotation to apply new keys", state.PID)))
```

Update imports for both files: add `"github.com/hopboxdev/hopbox/internal/ui"`.

**Step 3: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/hop/upgrade.go cmd/hop/rotate.go
git commit -m "style: use styled steps and warnings in hop upgrade and hop rotate"
```

---

### Task 12: Final verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Run linter**

Run: `golangci-lint run`
Expected: No new issues

**Step 3: Build all binaries**

Run: `make build`
Expected: All binaries compile

**Step 4: Verify no unused imports**

The refactoring removes `text/tabwriter` and `os` imports from several files. If the linter doesn't catch unused imports, `go build` will.
