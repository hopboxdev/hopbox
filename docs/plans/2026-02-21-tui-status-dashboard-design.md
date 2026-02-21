# TUI Status Dashboard Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:writing-plans to create an implementation plan from this design.

**Goal:** Replace the static `hop status` output with a live-updating Bubble Tea TUI showing tunnel health, services, and bridges.

**Tech Stack:** Go, charmbracelet/bubbletea, lipgloss

---

## Layout

Three sections stacked vertically:

```
╭─ Tunnel ───────────────────────────────────────╮
│ HOST: mybox          STATUS: ● connected       │
│ ENDPOINT: 51.38.50.59:51820                    │
│ PING: 14ms           UPTIME: 2h 34m           │
│ LAST HANDSHAKE: 3s ago                         │
│ SENT: 42.1 MB        RECEIVED: 128.3 MB       │
╰────────────────────────────────────────────────╯
╭─ Services ─────────────────────────────────────╮
│ NAME        STATUS     PORT   UPTIME           │
│ postgres    ● running  5432   2h 34m           │
│ redis       ● running  6379   2h 34m           │
│ api         ● running  8080   1h 12m           │
╰────────────────────────────────────────────────╯
╭─ Bridges ──────────────────────────────────────╮
│ clipboard   ● active   bidirectional           │
│ chrome-cdp  ● active   client → server         │
│ xdg-open    ● active   server → client         │
╰────────────────────────────────────────────────╯
                              q quit · r refresh
```

## Data Sources

- **Tunnel section:** Local state file (`tunnel.LoadState`) for connection status, uptime, endpoint. Agent `/health` endpoint ping for round-trip latency. WireGuard stats (handshake age, bytes) from `wgctrl` device query.
- **Services section:** Agent RPC `services.list` over the tunnel.
- **Bridges section:** Local bridge process state (already tracked during `hop up`).

## Refresh Strategy

- Poll every 5 seconds (tick-based Bubble Tea `Cmd`).
- `r` key forces immediate refresh.
- PING is measured as the actual round-trip time of the `/health` request.

## Command Integration

- `hop status` becomes the TUI (replaces current static output).
- Exit with `q` or `Ctrl+C`.
- No quick actions in v1 — just monitoring.

## Dependencies

- `charmbracelet/bubbletea` — TUI framework (Elm architecture)
- `charmbracelet/lipgloss` — Styling (borders, colors)
- Existing: `tunnel.LoadState`, agent `/health`, RPC `services.list`
