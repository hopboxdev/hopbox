# Bubbletea Step Runner Design

**Date:** 2026-02-21

## Goal

Replace the current `StepRun`/`StepOK` two-line pattern with animated bubbletea
spinners that resolve to checkmarks in place. Steps start with a spinning
indicator (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) and visually transform to ✔ on success or ✘ on
failure.

## Approach

Add bubbletea + bubbles as dependencies. Create `internal/tui/` with a reusable
`StepRunner` model. Each command defines its steps as data and hands them to the
runner. The runner handles animation, completion, errors, and TTY fallback.

Only multi-step commands use bubbletea (`hop setup`, `hop up`, `hop to`,
`hop upgrade`). List/status commands stay as plain `fmt.Println`.

## Architecture

### Package Layout

```text
internal/tui/
  step.go       — StepRunner model (Model/Update/View), Step type, messages
  styles.go     — lipgloss styles for TUI rendering
```

The existing `internal/ui/` package stays for non-TUI commands (Section, Table,
Dot, Row, Warn, Error). `StepOK`/`StepRun`/`StepFail` remain as the TTY
fallback.

### Dependencies Added

```
github.com/charmbracelet/bubbletea  (direct)
github.com/charmbracelet/bubbles    (direct, for spinner)
```

Both share transitive deps with lipgloss (already present): `charmbracelet/x/ansi`,
`charmbracelet/x/term`.

## Step Runner Model

### Step Type

```go
type Step struct {
    Title string
    Run   func(ctx context.Context, sub func(msg string)) error
}
```

- `Title` — displayed next to the spinner.
- `Run` — does the actual work. Runs in a goroutine (via `tea.Cmd`).
- `sub` — callback for sub-step messages. When called:
  1. The previous sub-step message (if any) is printed with ✔ via `tea.Println`.
  2. The spinner text updates to the new message.

### Model

```go
type stepRunner struct {
    ctx      context.Context
    steps    []Step
    results  []stepResult
    current  int
    spinner  spinner.Model
    subMsg   string         // current sub-step message shown on spinner line
    err      error
    program  *tea.Program   // for p.Send() from step goroutines
}
```

### Message Flow

```
Init()
  → spinner.Tick + runStep(0)

runStep(i) runs Step[i].Run in a goroutine
  → sub("connecting...") sends subStepMsg via p.Send()
  → sub("uploading...")   sends subStepMsg via p.Send()
  → step completes       sends stepDoneMsg{i, err}

Update(subStepMsg)
  → tea.Println previous sub-step with ✔
  → update spinner text to new message

Update(stepDoneMsg)
  → tea.Println final sub-step with ✔ (or ✘ if error)
  → mark results[i] = done
  → current++
  → runStep(current) or tea.Quit if last step
```

### View

Only the currently active line is rendered by View (inline mode, not altscreen):

```go
func (m stepRunner) View() string {
    return "  " + m.spinner.View() + " " + m.currentMessage()
}
```

All completed steps scroll up via `tea.Println` and persist in terminal
scrollback.

### Visual Output

```
  ✔ Connected to 51.38.50.59:22 as root       ← tea.Println (persisted)
  ✔ Uploaded hop-agent (4.2 MB)                ← tea.Println (persisted)
  ⠹ Generating server WireGuard keys...        ← View (animated)
```

When the last step completes:

```
  ✔ Connected to 51.38.50.59:22 as root
  ✔ Uploaded hop-agent (4.2 MB)
  ✔ Generated server WireGuard keys
  ✔ Exchanged client keys
  ✔ Started hop-agent service
  ✔ Host config saved. Bootstrap complete.
```

### TTY Fallback

Before creating `tea.NewProgram`, check `term.IsTerminal(os.Stdout.Fd())`.
If not a TTY (piped, redirected, CI), fall back to plain `ui.StepOK`/`ui.StepFail`
output. This keeps scripting compatibility.

```go
func RunSteps(ctx context.Context, steps []Step) error {
    if !term.IsTerminal(int(os.Stdout.Fd())) {
        return runStepsPlain(ctx, steps)
    }
    m := newStepRunner(ctx, steps)
    p := tea.NewProgram(m)
    m.program = p
    result, err := p.Run()
    // ...
}
```

## Command Integration

### `hop setup`

**Interactive parts handled before TUI:**
- SSH TOFU prompt (host key fingerprint confirmation)

**Approach:** Split Bootstrap into two phases:
1. SSH connection with TOFU — plain terminal I/O (before TUI)
2. Install + configure — inside TUI step runner

The `Bootstrap` function's `OnStep` callback wires to `p.Send(subStepMsg{...})`.
Each `logf` call inside Bootstrap becomes a sub-step that shows as a spinner
line, then resolves to ✔.

```go
// In setup.go
client, err := sshConnectWithTOFU(ctx, opts, os.Stdout) // plain I/O
if err != nil { return err }
defer client.Close()

steps := []tui.Step{
    {Title: "Setting up " + c.Name, Run: func(ctx context.Context, sub func(string)) error {
        return bootstrapWithClient(ctx, client, opts, sub)
    }},
}
return tui.RunSteps(ctx, steps)
```

### `hop up`

**Steps phase:**
1. "Bringing up tunnel" — creates TUN, configures interface
2. "Probing agent" — health check
3. "Syncing manifest" — workspace sync
4. "Installing packages" — package install

**Monitoring phase:** After steps complete, the model transitions to a
monitoring state. The ConnMonitor sends events via `p.Send()`. The View
shows connection status updated in real-time:

```
  ✔ Interface utun4 up, 10.10.0.1 → mybox.hop
  ✔ Agent is up
  ✔ Manifest synced
  ✔ Packages installed

  ● connected — Press Ctrl-C to stop
```

When disconnected:

```
  ● disconnected — waiting for reconnection...
```

On reconnect:

```
  ● connected — reconnected (was down for 12s)
```

Ctrl-C sends `tea.KeyMsg` → cleanup → `tea.Quit`.

### `hop to`

**Interactive part before TUI:** "Proceed? [y/N]" confirmation.

**Steps in TUI:** Flat list with group headers emitted via `tea.Println`:

```go
steps := []tui.Step{
    {Title: "Creating snapshot on " + sourceHost, Run: createSnapshot},
    {Title: "Setting up " + c.Target, Run: bootstrapTarget}, // sub-steps via callback
    {Title: "Connecting to " + c.Target, Run: connectTarget},
    {Title: "Restoring snapshot", Run: restoreSnapshot},
    {Title: "Setting default host", Run: switchDefault},
}
```

Group headers ("Step 1/4 Snapshot", etc.) are printed by each step's Run
function via `sub()` or emitted before running.

### `hop upgrade`

```go
steps := []tui.Step{
    {Title: "Checking for latest release", Run: checkRelease},  // if needed
    {Title: "Upgrading client", Run: upgradeClient},
    {Title: "Upgrading helper", Run: upgradeHelper},            // if darwin
    {Title: "Upgrading agent", Run: upgradeAgent},              // sub-steps
}
```

The helper upgrade needs `sudo`. Use `tea.Exec` to hand terminal control to
the `sudo hop-helper --install` subprocess, then resume the TUI.

## Handling Special Cases

### Progress Bar (Agent Upload)

The `progressReader` in `install.go` currently writes `\r`-based progress bars.
With bubbletea, it sends progress messages via `p.Send()`:

```go
type progressMsg float64 // 0.0 to 1.0
```

The View shows a text-based progress bar below the spinner:

```
  ⠹ Uploading hop-agent...
    [██████████████░░░░░░░░░░░░░░░░] 47%
```

The progressReader needs a reference to `*tea.Program` (passed via the step's
closure or stored on a struct).

### `sudo` Prompts (`hop upgrade` helper)

Use `tea.ExecProcess`:

```go
func upgradeHelper(ctx context.Context, sub func(string)) error {
    sub("Installing helper (requires sudo)")
    // ... download/prepare binary ...
    // Hand terminal to sudo
    return execSudo(tmpPath)  // returns tea.ExecProcess cmd
}
```

This suspends the TUI, runs `sudo`, then resumes. The TUI picks up where it
left off.

Actually, since `tea.ExecProcess` is a `tea.Cmd` (not callable from within a
step's Run goroutine), the helper upgrade step would need to return a special
message that triggers `tea.ExecProcess` in Update. The step runner handles this
by checking for a `needExecMsg` type.

### Reconnection Monitor (`hop up`)

The ConnMonitor runs as a background goroutine. It sends events to the TUI
via `p.Send()`:

```go
monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
    OnStateChange: func(evt tunnel.ConnEvent) {
        p.Send(connStateMsg(evt))
    },
    OnHealthy: func(t time.Time) {
        p.Send(healthyMsg(t))
    },
})
```

The model's Update handles these messages to update the status display.

## What Changes

| File | Change |
|------|--------|
| `internal/tui/step.go` | New: StepRunner model |
| `internal/tui/styles.go` | New: shared lipgloss styles |
| `internal/setup/bootstrap.go` | Refactor: extract SSH connect phase, accept SSH client |
| `internal/setup/install.go` | Refactor: progressReader sends via p.Send() or callback |
| `cmd/hop/setup.go` | Rewrite: use tui.RunSteps |
| `cmd/hop/up.go` | Rewrite: use tui.RunSteps + monitoring phase |
| `cmd/hop/to.go` | Rewrite: use tui.RunSteps |
| `cmd/hop/upgrade.go` | Rewrite: use tui.RunSteps |

## What Does NOT Change

- `internal/ui/` — stays for non-TUI commands (Section, Table, Dot, etc.)
- `cmd/hop/statusview.go` — single render, no animation
- `cmd/hop/services.go`, `bridge.go`, `host.go`, `snap.go` — list commands
- `cmd/hop/rotate.go` — too simple for TUI (one warning + one call)
- `hop version`, `hop init`, `hop logs` — no step progress
- Error return values — Kong handles error display
- `internal/tunnel/monitor.go` — unchanged, just wired to p.Send()
