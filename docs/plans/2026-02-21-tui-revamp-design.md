# TUI Revamp Design

## Goal

Replace the current minimal step runner with a rich progress display that shows
all sub-steps expanded, phase grouping, a step counter, and clear error/warning
states. Non-interactive (except existing pre-TUI prompts). Charm ecosystem
aesthetics using existing Bubble Tea / Lipgloss dependencies.

## Data Model

```go
type Phase struct {
    Title string
    Steps []Step
}

type Step struct {
    Title    string
    Run      func(ctx context.Context, send func(StepEvent)) error
    NonFatal bool // if true, error renders as warning and execution continues
}

type StepEvent struct {
    Message string // progress update shown next to spinner
}
```

Entry point:

```go
func RunPhases(ctx context.Context, title string, phases []Phase) error
```

## Rendering

Mid-execution:

```
Setting up mybox [3/7]

SSH Bootstrap
  ✔ Connected to 1.2.3.4
  ✔ Uploaded agent binary (4.2 MB)
  ⠦ Generating WireGuard keys...

WireGuard Setup
  ○ Configure tunnel
  ○ Start systemd service

Verification
  ○ Probe agent health
  ○ Set default host
```

On failure:

```
Setting up mybox [3/7]

SSH Bootstrap
  ✔ Connected to 1.2.3.4
  ✔ Uploaded agent binary (4.2 MB)
  ✘ Generating WireGuard keys
    Error: permission denied: /etc/hopbox/agent.key
```

Non-fatal warning:

```
  ⚠ Sync manifest
    Warning: hopbox.yaml not found (skipped)
```

Completed:

```
Setting up mybox [7/7]

SSH Bootstrap
  ✔ Connected to 1.2.3.4
  ✔ Uploaded agent binary (4.2 MB)
  ✔ Generated WireGuard keys

WireGuard Setup
  ✔ Configured tunnel
  ✔ Started systemd service

Verification
  ✔ Agent healthy (v0.4.2)
  ✔ Default host set to "mybox"
```

### Styling

- Title: bold, step counter in subtle/gray
- Phase headers: bold, no icon
- `✔` completed: green
- `⠦` active: yellow spinner (animated)
- `○` pending: subtle/gray
- `✘` failed: red, error message indented below in red
- `⚠` non-fatal: yellow, warning message indented below in yellow
- One blank line between phases

### Non-TTY Fallback

No spinner, no counter, no phase headers. Sequential checkmarks:

```
Setting up mybox
  ✔ Connected to 1.2.3.4
  ✔ Uploaded agent binary (4.2 MB)
  ...
```

## State Machine (Bubble Tea)

```go
type runner struct {
    title      string
    phases     []Phase
    steps      []flatStep   // flattened across all phases
    current    int
    totalSteps int
    spinner    spinner.Model
    err        error
    done       bool
    program    *tea.Program
    ctx        context.Context
    cancel     context.CancelFunc
}

type flatStep struct {
    phaseIdx int
    title    string
    status   status   // pending | running | done | failed | warned
    message  string   // last StepEvent message
    errMsg   string   // error or warning detail
    nonFatal bool
}

type status int
const (
    statusPending status = iota
    statusRunning
    statusDone
    statusFailed
    statusWarned
)
```

Messages: `stepEventMsg`, `stepDoneMsg`, `stepFailMsg`, `spinner.TickMsg`.

Lifecycle:
1. `Init()` -- start spinner, launch first step goroutine
2. `Update()` -- handle messages, advance steps, detect completion
3. `View()` -- render title + counter, phases with steps using `phaseIdx`

Ctrl+C cancels context. Panics in step goroutines recovered and converted to
`stepFailMsg`.

## Integration

Each CLI command defines `[]Phase` and calls `tui.RunPhases()`.

- `hop setup`: 3 phases (SSH Bootstrap, WireGuard Setup, Verification), ~7 steps
- `hop up`: 2-3 phases, ~4 steps
- `hop to`: 3-4 phases, ~6 steps
- `hop upgrade`: 1-2 phases, conditional steps

State shared between steps via closures (same pattern as today).
Pre-TUI prompts (TOFU, sudo) remain as stdout interactions before `RunPhases`.

## Error Handling

- **Step failure:** Mark `statusFailed`, render `✘` + error, stop. Return error.
- **Non-fatal failure:** `Step.NonFatal = true`. Render `⚠` + warning, continue.
- **Ctrl+C:** Cancel context, exit cleanly.
- **Panic:** Recover in goroutine, convert to `stepFailMsg`.
- **Empty phases:** Omitted from display. Counter only counts actual steps.

## Files Changed

- `internal/tui/runner.go` -- new (replaces `step.go`)
- `internal/tui/runner_test.go` -- new (replaces `step_test.go`)
- `cmd/hop/setup.go` -- migrate to phases
- `cmd/hop/up.go` -- migrate to phases
- `cmd/hop/to.go` -- migrate to phases
- `cmd/hop/upgrade.go` -- migrate to phases
- `internal/tui/step.go` -- deleted
- `internal/tui/step_test.go` -- deleted

## Dependencies

None new. Uses existing Bubble Tea, Bubbles, Lipgloss.
