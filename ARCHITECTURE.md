# hopbox — architecture map

> **hopbox is a compute-box substrate**: on-demand, SSH-gated, isolated boxes
> (Firecracker microVMs / docker containers), shared by humans and AIs, with a
> built-in AI-control plane (MCP + the canvas loop). This repo is *only* the
> substrate — the dev-env / web-app layer that will sit on top lives in a
> separate repo (see "What's not here").

## The layers

```
        ┌──────────────────────────────────────────────────────────────┐
        │  actors:   human (ssh box@host)   ·   AI (MCP plane)           │
        └───────────────┬───────────────────────────┬──────────────────┘
                        │                            │
 ╔══════════════════════╪════════════════════════════╪═════════════════════╗
 ║  hopboxd  — the daemon (the substrate control plane)                     ║
 ║  · SSH front door (sshfront)      · agent hub (reverse tunnel)           ║
 ║  · box metadata API (boxmeta)     · box fleet = store + reconciler       ║
 ║  · AI-control MCP plane (internal/mcp) + canvas surfaces                 ║
 ╠═════════════════════════════════════════════════════════════════════════╣
 ║  CORE  (internal/core/box)  — the box engine                            ║
 ║  · box.Engine / box.Store (boxsqlite) / box.Reconciler  (desired→actual)║
 ║  · provider contracts (internal/core/ports) → providers/compute/*       ║
 ╠═════════════════════════════════════════════════════════════════════════╣
 ║  A BOX  — one isolated machine                                          ║
 ║  Firecracker microVM · docker container                                 ║
 ║  runs: hopbox-agent (init) → agentssh (in-box sshd) + box-guest         ║
 ╚═════════════════════════════════════════════════════════════════════════╝
```

**In one breath:** a **box** is one sandbox; **hopboxd** is the daemon that spawns
boxes and lets humans SSH in and AIs drive them; the **core** box engine turns
desired state into running boxes on a compute backend.

## Vocabulary (every name, one line)

### The daemon & binaries
| Binary | Job |
|---|---|
| **hopboxd** | The hopbox daemon. `ssh box@host` → spawns a box and drops you in. Serves the MCP plane + canvas surfaces. Both compute backends compiled in; `--compute docker\|microvm`. |
| **hopbox-mcp** | The AI-control plane client: `ps` glance, `watch`, `--connect` bridge, `--demo`. |
| **hopbox-agent** | Runs as a box's init; dials home to the agent hub (reverse tunnel). |
| **box-guest** | In-box CLI hitting the metadata API (keep-alive, `status`, `mcp`). |

### Daemon-side packages
| Package | Job |
|---|---|
| **internal/core/box** | Box lifecycle: `Engine` / `Store` / `Reconciler` (desired → actual). |
| **internal/core/boxsqlite** | The box-native sqlite store (no dev-env coupling). |
| **internal/core/boxmeta** | The in-box metadata API (`169.254.169.254`): identity, status, time. |
| **internal/core/ports** | Provider contracts (Go interfaces): `Compute`. |
| **internal/agenthub** | Where in-box agents connect back; multiplexes shell/exec/ssh streams. |
| **internal/agentssh** | The real SSH server that runs **inside** a box (shell/exec/sftp). |
| **internal/sshfront** | The SSH **front door**: external `ssh` → agenthub → the box's `agentssh`. |
| **internal/sshca** / **agentproto** | Front-door host CA; agent wire protocol. |
| **internal/mcp** | The MCP server: `hopbox://fleet` + `hopbox://surface/*` resources, `box.delegate` / `fleet.apply` / `surface.render` tools, pushed events, the `instructions` guide. |
| **providers/compute/{docker,microvm}** | The two compute backends. |

### The AI-control plane (design/ai-control-protocol.md)
`hopbox://fleet` (live box state) · `box.delegate` / `fleet.apply` (run/converge) ·
**the canvas loop** (`surface.render` + `hopbox://surface/{name}/events`) — the AI
renders an interactive UI and is *pushed* the human's interactions.

## Request flows

**A. `ssh box@host` → a shell in a box**
```
ssh client → hopboxd:22 (sshfront) → identify by key → box.Engine spawns/attaches a box
           → hopbox-agent (in box) has dialed agenthub → sshfront proxies your session
           → agentssh (in box) gives you the shell.   scp/sftp/rsync ride the same path.
```

**B. AI delegates work**
```
AI (MCP client) → hopboxd MCP socket → internal/mcp (EngineBackend)
   box.delegate → box.Engine.Attach → agenthub.OpenExec → runs the task
   → result pushed on hopbox://fleet.   fleet.apply = the same, declaratively, for a set.
```

**C. AI ↔ human via a canvas**
```
AI → surface.render{html} → hopboxd --surface-addr serves /s/<name> (+ capture JS)
   → human opens URL, clicks → pushed on hopbox://surface/<name>/events → AI reacts.
```

## What's not here (parked / separate repo)

The **dev-env / web-app layer** — persistent-home *workspaces*, accounts, HTTP
ingress (`hopbox-gw`), the remote-provider plugin system, k8s backends, the `hopbox`
client CLI — was split out. It's preserved on the **`parked/devenv`** branch and
will become its own repo, built *on top of* this substrate. See
`memory/strategy-hopbox-substrate.md`.

## Naming note

**hopbox** now means exactly one thing: the substrate (this repo + the daemon
`hopboxd`). The old overload (where "hopbox" also meant the dev-env layer) is gone.
"box" = a single sandbox; there is no separate "workspace" concept in the substrate.
