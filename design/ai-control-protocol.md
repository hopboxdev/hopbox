# hopbox AI-control protocol (design)

**Status:** design + spine prototype (`cmd/hopbox-mcp`). Not wired into the product yet.

## Why not a CLI

The obvious move — mirror herdr's `herdr <noun> <verb>` CLI — is wrong for hopbox.
herdr is a CLI *because it is terminal-native*: the agent already lives in a shell
pane, so shelling out is the path of least resistance. hopbox is a **fleet of
remote boxes**; copying the CLI copies herdr's form-factor accident, not its
insight. herdr's real idea is that **the AI drives the same live control plane the
human GUI uses** — one shared, observable, mutable workspace. The interface is
incidental; the shared live plane is the point.

So hopbox's native control surface is a **protocol**, not a command line.

## Principles

1. **MCP-native.** Typed tools + typed, discoverable resources. A CLI, if it
   exists, is a thin human/script fallback *generated from* this — never the source.
2. **Event-driven, not request/response.** No polling. The plane *pushes* state
   changes; the AI reacts to a stream. A nervous system, not a phone to re-dial.
3. **Declarative.** The AI states intent (`fleet.apply`); hopbox's **existing
   reconciler** converges reality and reports. Goals in, bookkeeping handled.
4. **One plane, many actors.** Humans, the in-box agents, and any number of AIs
   read/subscribe/act on the *same* resources — like herdr's GUI+CLI on one socket,
   generalized to a fleet.
5. **Bidirectional.** `box-guest` inside a box reports *up* over the same protocol
   (it writes `hopbox://box/{self}/agent` and `…/surface/events`); clients read down.

## The model: Resources + Tools + Notifications (the three MCP primitives)

### Resources — live state (the backbone)
Everything addressable is a URI; **every resource is subscribable**
(`resources/subscribe` → `notifications/resources/updated`). Read = snapshot;
subscribe = live feed. This one mechanism is the dashboard refresh, the log tail,
and the canvas feedback loop.

| URI | Contents |
|---|---|
| `hopbox://fleet` | every box + workspace + agent state — the glance |
| `hopbox://workspace/{ws}` | a workspace and its boxes |
| `hopbox://box/{id}` | phase, image, cpu/mem, ip, load, idle, keep_alive, status |
| `hopbox://box/{id}/logs` | rolling stdout/stderr (subscribe = `tail -f`) |
| `hopbox://box/{id}/agent` | the in-box agent's self-report: `state(working\|blocked\|done)` + message |
| `hopbox://box/{id}/fs/{path}` | files / dirs (read + browse) |
| `hopbox://box/{id}/surface/{name}` | a rendered UI surface + its URL |
| `hopbox://box/{id}/surface/{name}/events` | interaction stream (clicks/inputs) — the canvas loop |

### Tools — verbs (JSON-Schema'd)
```jsonc
// lifecycle
box.spawn   { name?, image?, cwd?, env?, workspace?, persist? }  -> { id, endpoint? }
box.destroy { id }   box.rename { id, name }   box.keep_alive { id, dur }
box.suspend { id }   box.resume { id }

// work & delegation — RETURN IMMEDIATELY, never block
box.run      { id, cmd }                 -> { exec_id }   // output streams into …/logs
box.delegate { task, image?, name? }     -> { id }        // spawn a box + run an agent/task on it
box.send     { id, text }                -> ok            // input to a running session

// files & editor
box.read { id, path }   box.write { id, path, content }   box.patch { id, diff }
box.open_editor { id, path? }            -> { vscode_uri } // "jump to file/diff in VS Code"

// surfaces / canvas
box.expose     { id, port }              -> { url }        // gateway endpoint
surface.render { id, name, html|app }    -> { url }        // AI renders a canvas
                                                           // interactions arrive via …/surface/events

// declarative — the big one
fleet.apply { spec }   // desired boxes/agents/tasks -> reconciler converges
fleet.get              // observed state (== hopbox://fleet)
```
**Delegation never blocks.** `box.delegate` returns an id; the AI *subscribes* to
`hopbox://box/{id}/agent` and reacts on `done`/`blocked`. There is deliberately no
`wait` tool — the event stream replaces it.

### Notifications — events (push, the nervous system)
```
notifications/resources/updated  hopbox://box/{id}/agent     // working->blocked (needs you) or ->done
notifications/resources/updated  hopbox://fleet              // a box appeared / reaped / changed
notifications/resources/updated  …/surface/{name}/events     // human clicked/typed on a canvas
```
Semantic events the platform emits: `box.ready`, `box.finished`, `agent.blocked{needs:input|approval}`, `box.reaped`, `surface.interaction`.

## The three "products" all fall out of one surface

- **Orchestration** — `box.delegate(task)` → id; subscribe `…/agent`; react on
  `done`/`blocked`. Fan out N, react as each lands. No `wait`, no poll.
- **The herdr glance** — subscribe `hopbox://fleet`; the GUI, an optional CLI, and
  the AI all render the *same* live tree with agent status. The dashboard is a subscriber.
- **The canvas loop** — `surface.render(...)` → user opens the URL → subscribe
  `…/surface/events` → react to clicks. "AI observes interaction" is a subscription.

## Topology

```
   AI client (Claude, …) ──MCP──┐
   GUI / dashboard ─────────────┤   hopbox control plane  ──►  boxd / reconciler ──► boxes
   optional CLI ────────────────┘   (the MCP server)      ◄──  box-guest reports up
```
The MCP server *is* the hopbox control plane (hopboxd). The AI connects as an MCP
client; `box-guest` is the in-box reporter writing to the same plane; the GUI is
another client. One protocol, three kinds of actor.

## Prototype (the spine)

`cmd/hopbox-mcp` implements the smallest end-to-end slice that proves the model:
- the `hopbox://fleet` **resource** (read + subscribe),
- the `box.delegate` / `box.spawn` / `fleet.get` **tools**,
- `notifications/resources/updated` **pushed** when a box changes state.

`hopbox-mcp --demo` self-drives it over an in-memory MCP pipe against **real boxes
on box.hopbox.dev**: it subscribes to the fleet, delegates two tasks, and is
*pushed* each completion — the client never polls. See the run in the PR.
