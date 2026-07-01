# hopbox — architecture map

> One canonical picture of what each thing is, so the names stop being blurry.
> If you only read one section, read **The three layers** and **Vocabulary**.

## The three layers

```
        ┌──────────────────────────────────────────────────────────────┐
        │  actors:   human (ssh / hopbox CLI)   ·   AI (MCP plane)       │
        └───────────────┬───────────────────────────┬──────────────────┘
                        │                            │
 ╔══════════════════════╪════════════════════════════╪═════════════════════╗
 ║  DEV-ENV LAYER  ("hopbox")            daemons: hopboxd + hopbox-gw        ║
 ║  a box + persistent /home + ingress + account = a WORKSPACE              ║
 ║  · workspace (embeds a box)   · account (who owns it)                    ║
 ║  · providers: storage/home, ingress, identity, metering, build          ║
 ║  · gRPC API  ← the `hopbox` CLI    · hopbox-gw = HTTP(S) ingress         ║
 ╠═════════════════════════════════════════════════════════════════════════╣
 ║  COMPUTE LAYER  ("boxd")               daemon: boxd                      ║
 ║  spawn & shepherd boxes, SSH front door, reap/suspend                   ║
 ║  · SSH front door :22 (sshfront)   · agent hub (reverse tunnel)          ║
 ║  · box metadata API (boxmeta)      · fleet = store + reconciler          ║
 ╠═════════════════════════════════════════════════════════════════════════╣
 ║  CORE  (internal/core/box)   — the SHARED substrate both daemons use     ║
 ║  · box.Engine / box.Store / box.Reconciler  (desired → actual)          ║
 ║  · provider contracts (internal/core/ports) satisfied by providers/*    ║
 ╠═════════════════════════════════════════════════════════════════════════╣
 ║  A BOX  — one isolated machine                                           ║
 ║  Firecracker microVM · docker container · k8s pod                        ║
 ║  runs: hopbox-agent (init) → agentssh (in-box sshd) + box-guest          ║
 ╚═════════════════════════════════════════════════════════════════════════╝
```

**In one breath:** a **box** is one sandbox; **boxd** is the daemon that makes boxes and lets you SSH into them; **hopbox** is the dev-env product built on boxes (persistent home + ingress + accounts), where the unit is a **workspace** (a box with an identity). Both daemons stand on the same **core** box engine.

## Vocabulary (every name, one line)

### The units
| Term | What it is |
|---|---|
| **box** | One isolated machine (µVM / container / pod). The atomic compute unit. Ephemeral or persistent. |
| **workspace** | The **dev-env** unit: a `box` **plus** a persistent `/home`, ingress, and an owning account. `workspace` embeds `box.Box`. |

### The daemons & user-facing binaries
| Binary | Layer | Job |
|---|---|---|
| **boxd** | compute | Standalone box daemon. `ssh box@host` → spawns a box and drops you in. Runs `box.hopbox.dev`. |
| **hopboxd** | dev-env | The dev-env control plane: store + reconciler + agent hub + gRPC API. Serves workspaces. |
| **hopbox-gw** | dev-env | Stateless HTTP(S) service gateway — public ingress to a workspace's ports (subdomains). |
| **hopbox** | dev-env | The user CLI: `create / ls / rm / shell` against hopboxd. |
| **hopbox-provider** | either | Serves one provider out-of-process over gRPC (remote compute/storage/…). |

### Inside a box
| Term | Job |
|---|---|
| **hopbox-agent** | Runs as the box's init; dials home to the daemon's agent hub (reverse tunnel). |
| **agentssh** | The real SSH server **inside** the box — shell / exec / sftp. |
| **box-guest** | In-box CLI hitting the metadata API (keep-alive, `status`, `mcp`, …). |
| **boxmeta** | The in-box metadata API at `169.254.169.254` (identity, status, time). |

### Daemon-side plumbing
| Term | Job |
|---|---|
| **agenthub** | Where in-box `hopbox-agent`s connect back; multiplexes shell/exec/ssh streams. |
| **sshfront** | The SSH **front door**: external `ssh` → agenthub → the box's `agentssh`. |
| **box.Engine / Store / Reconciler** | Core box lifecycle: desired state → actual (provision, reap, suspend, resume). |
| **ports** | Provider **contracts** (Go interfaces): compute, storage, ingress, identity, metering, build. |
| **providers/** | Implementations: compute {docker, microvm, kubernetes}, storage {localfs, kubernetes}, ingress {subdomain}, identity {oidc, static}, … |
| **plugin** | The provider adapter + loader (in-process or gRPC via hopbox-provider). |
| **account** | The dev-env's registered-key directory — who owns which workspace. |

### The AI-control plane (new — PRs #63–68)
| Term | Job |
|---|---|
| **internal/mcp** | The MCP server: `hopbox://fleet` + `hopbox://surface/*` resources, `box.delegate` / `fleet.apply` / `surface.render` tools, pushed change events, and the `instructions` guide. |
| **hopbox-mcp** | The client/entrypoint: a standalone server, `--connect` bridge, `ps` glance, `watch`, `--demo`. |
| **canvas loop** | `surface.render` an interactive UI → user interacts → the AI is *pushed* the events. Served by `boxd --surface-addr`. |

## Two daemons, one core

`boxd` and `hopboxd` are **not** a rewrite of each other — they wrap the *same* `internal/core/box` engine, store, and reconciler, plus the same agent hub / agentssh / sshfront path. They differ in what they add **on top**:

- **boxd** adds only the front door + boxmeta → "compute boxes via SSH." No accounts, no persistent home, no ingress. (krillbox-parity.)
- **hopboxd** adds the **dev-env**: `workspace` (identity for a box), `account` (ownership), storage providers (persistent `/home`), ingress (`hopbox-gw`), metering, and a gRPC API for the `hopbox` CLI.

On `box.hopbox.dev` today both run on the VPS, **converged** on the shared `box.Reconciler` (boxd = the public SSH front door; hopboxd + hopbox-gw = the dev-env, tailnet-only).

## Request flows (follow the arrows)

**A. `ssh you@box.hopbox.dev` → a shell in a box** (compute layer)
```
ssh client → boxd:22 (sshfront) → identify by key → box.Engine spawns/attaches a box
           → hopbox-agent (in box) has dialed agenthub → sshfront proxies your session
           → agentssh (in box) gives you the shell.   scp/sftp/rsync ride the same path.
```

**B. AI delegates work** (AI-control plane)
```
AI (MCP client) → boxd MCP socket → internal/mcp → EngineBackend
   box.delegate → box.Engine.Attach (real box) → agenthub.OpenExec → runs the task
   → result pushed on hopbox://fleet.   fleet.apply = the same, declaratively, for a set.
```

**C. AI ↔ human via a canvas** (canvas loop)
```
AI → surface.render{html} → boxd --surface-addr serves /s/<name> (+ capture JS)
   → human opens URL, clicks → POST /s/<name>/event → pushed on hopbox://surface/<name>/events
   → AI reacts (re-render, unblock a task, …).
```

**D. `hopbox create` → a dev-env workspace** (dev-env layer)
```
hopbox CLI → hopboxd gRPC → workspace created (embeds a box) → storage provider mounts a
   persistent /home → box.Reconciler brings the box up → ingress provider + hopbox-gw expose
   its ports on a subdomain → `hopbox shell` drops you in (same agent path as A).
```

## Naming tensions (open — for discussion)

These are the places the words fight each other. Worth resolving before more surface area piles on:

1. **"hopbox" does double duty** — the umbrella brand/repo *and* the specific dev-env layer. When you say "hopbox" you might mean either. This is the #1 source of fog.
2. **`boxd` vs `hopboxd`** — one syllable apart, indistinguishable out loud, and both are "the control plane" for their layer.
3. **"agent" is overloaded** — `hopbox-agent` (box init), `agenthub`, `agentssh`, *and* the AI agent that drives the MCP plane. Four unrelated meanings.
4. **box vs workspace** — actually the *clearest* pair (unit vs identity-wrapped unit), but only if we use them consistently and never say "box" when we mean "workspace."

> The dev-env layer is where most of this knots up (workspace vs box, hopboxd vs boxd, brand vs layer). That's the conversation to have next.
