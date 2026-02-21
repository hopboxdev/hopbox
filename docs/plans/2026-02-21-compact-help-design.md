# Compact Help Output

## Problem

`hop help` lists 24 command lines because subcommands (services ls/restart/stop,
host add/rm/ls/default, snap create/restore/ls, bridge ls/restart) are expanded
inline. This makes the output feel bloated.

## Solution

Add `kong.ConfigureHelp(kong.HelpOptions{NoExpandSubcommands: true, Compact: true})`
to the Kong constructor in `cmd/hop/main.go`.

- `NoExpandSubcommands` collapses commands with subcommands into a single line.
- `Compact` removes blank lines between commands and hides argument signatures
  from the top-level listing.

Together these reduce the top-level help from ~24 verbose lines to 16 compact lines.

## Changes

- `cmd/hop/main.go`: Add `kong.ConfigureHelp(...)` option to `kong.New()` call.

## Behavior

- Top-level `hop help`: subcommands collapsed (e.g., `services ...`)
- Per-command `hop services --help`: still shows full subcommand listing
- No breaking changes to CLI behavior
