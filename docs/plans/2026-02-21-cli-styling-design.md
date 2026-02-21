# CLI Styling Design

**Date:** 2026-02-21

## Goal

Extract styling from `statusview.go` into a shared `internal/ui` package and
apply consistent styled output across all CLI commands.

## Approach

Create `internal/ui` with reusable lipgloss primitives. Refactor
`statusview.go` to consume them. Update list commands to use bordered sections
with colored dots. Update lifecycle commands to use styled step output.

## `internal/ui` Package

### Color Palette

```go
Green  = lipgloss.Color("2")  // connected, running, success
Red    = lipgloss.Color("1")  // disconnected, error
Yellow = lipgloss.Color("3")  // stopped, in-progress, warning
Subtle = lipgloss.Color("8")  // borders, column headers
```

### Primitives

| Primitive                         | Returns  | Purpose                                    |
|-----------------------------------|----------|--------------------------------------------|
| `Section(title, content, width)`  | `string` | Bordered box with bold title               |
| `Dot(state)`                      | `string` | Colored `●` (green/red/yellow)             |
| `Row(k1, v1, k2, v2, width)`     | `string` | Two-column key-value row                   |
| `Table(headers, rows, width)`     | `string` | Columnar data with subtle headers          |
| `StepOK(msg)`                     | `string` | `✔ msg` in green                           |
| `StepRun(msg)`                    | `string` | `○ msg` in yellow                          |
| `StepFail(msg)`                   | `string` | `✘ msg` in red                             |
| `Warn(msg)`                       | `string` | `⚠ msg` in yellow (for stderr)             |
| `Error(msg)`                      | `string` | `✘ msg` in red (for stderr)                |
| `MaxWidth`                        | `int`    | Constant `80`                              |

All functions return strings. Callers decide where to print (stdout vs stderr).

### State Constants for `Dot`

```go
StateConnected    // green
StateDisconnected // red
StateStopped      // yellow
```

## Commands to Update

### Status (`statusview.go`)

Refactor to import `ui.Section`, `ui.Dot`, `ui.Row`. No visual changes — pure
extraction. Delete color/style vars from `statusview.go`.

### List Commands

**`hop services ls`** — Replace `tabwriter` with `ui.Section` + `ui.Table` +
`ui.Dot`. Green dot for running, yellow for stopped, red for error. Empty
state: "No services." inside the section.

**`hop bridge ls`** — Same bordered section. Dots for status.

**`hop host ls`** — Bordered section. Green dot on default host, no dot on
others. "(default)" label next to the default.

**`hop snap ls`** — Parse JSON response (currently raw `CallAndPrint`). Render
with `ui.Section` + `ui.Table`. Columns: ID, CREATED (relative time).

### Lifecycle Commands

No header boxes — steps print immediately.

**`hop up`** — Replace `fmt.Printf`/`Println` calls with `ui.StepOK` /
`ui.StepRun`. Warnings use `ui.Warn` on stderr. Reconnection monitor messages
keep their current timestamped format (runtime events, not setup steps).

**`hop setup`** — `setup.Bootstrap` currently takes `io.Writer`. Change to
accept a `func(string)` callback for step notifications. Caller wraps with
`ui.StepOK`.

**`hop to`** — Numbered step groups with `ui.StepOK` lines underneath.
Warnings use `ui.Warn`.

### Warnings and Errors

Replace all `fmt.Fprintf(os.Stderr, "Warning: ...")` with `ui.Warn(msg)`.
Replace error prefixes with `ui.Error(msg)` where applicable. Both render
colored output; callers write to stderr.

## What Does NOT Change

- Error return values — Kong handles `error` display, stays plain
- `hop logs` — raw passthrough streaming, no styling
- `rpcclient.CallAndPrint` for action commands (`services restart`,
  `services stop`, `snap restore`) — these return confirmation text, not
  tabular data
- `hop version` — single line, no styling needed
- `hop init` — scaffold generation, no styling needed
