# Box product — feature roadmap & specs

Design notes for evolving the standalone compute-box product (`boxd` + `core/box`)
toward parity with the reference "compute-boxes-over-SSH" product we studied
(krillbox/krillsh). This is an internal planning doc, not user docs.

## Principles (learned from the reference)

1. **SSH is the whole interface. No CLI, no management syntax.** The reference
   has *no* `list`/`create`/`rm` — boxes are born from `ssh box@host` and die
   from idle. Resist adding a management surface.
2. **Identity = the SSH key; box identity = its network position.** The box
   metadata API identifies a box by *source IP*, so there is **no credential in
   the box** (no token-in-env to steal — unlike hopbox today).
3. **Lifecycle is automatic; the owner only *tunes* it.** Idle → suspend;
   traffic → wake; the owner can keep-alive / adjust idle thresholds — nothing
   else. There is no manual destroy.
4. **The substrate is microVMs.** Hardware isolation makes anonymous-root boxes
   safe by construction, and it's the only practical substrate for
   suspend/snapshot/wake/clone. Containers are the dev/single-node fallback.
5. **AI-agent-native.** The in-guest tool can run an MCP server, so an agent can
   manage its own sandbox.

## Current state (what we have)

`core/box` (grammar, backend, model, lifetime, flavor, engine, reconciler);
`boxd` (standalone, persistent `boxsqlite`, docker provider, network self-fence);
ephemeral + persistent boxes; cpu/mem flavors; `ports.Compute` seam.

Gaps vs the reference: microVMs, metadata-identity, idle/suspend/wake, image
cloning, in-guest tool, MCP, rootfs flavors.

---

## Features

Each: **What / Why / Design (mapped to our seams) / Effort / Risk / Deps.**

### F1 — microVM compute provider  *(the moat)*
- **What:** a `ports.Compute` provider backed by Firecracker (a VMM). A box is a microVM, not a container.
- **Why:** hardware-virt isolation ⇒ anonymous-root is safe by construction
  (dissolves the iptables/cgroup hardening); the only good substrate for
  suspend/snapshot/wake/clone; matches the reference substrate (microVMs) and the
  AI-untrusted-code use case.
- **Design:** new `providers/compute/microvm`. `Provision` = build a CoW rootfs
  overlay from a golden image (F6), boot kernel+rootfs under the VMM with a tap
  device on a managed bridge (e.g. `10.0.0.0/24`); the VM gets an IP; the agent
  runs inside (as PID-1 init, F9) and reverse-dials the hub. `Status/Stop/Destroy`
  map to VMM lifecycle.
- **Effort:** L (weeks). **Risk:** high. **Deps:** KVM host. Foundation for
  F4(real)/F5/F6/F9.
- **VMM:** **Firecracker** — minimal, battle-tested, suspend/snapshot built in.
  (GPU is out of scope — no hardware access — which removes the only reason to
  reach for Cloud Hypervisor.)

### F2 — Metadata-API identity + in-guest tool  *(management model + security fix)*
- **What:** a control-plane HTTP metadata server the box reaches at a link-local
  address (e.g. `169.254.169.254`), serving `/v1/me`, `/v1/me/time`, and the
  lifecycle controls. A small `box-guest` binary in the box image is a thin
  client. The box is identified by **source IP**, not a token.
- **Why:** removes hopbox's `HOPBOX_AGENT_TOKEN`-in-env wart (a box root user can
  read it today); establishes the *only* owner surface (per-box tuning); the
  foundation for F3/F4-controls/F7. **Works on Docker now — not gated on microVM.**
- **Design:** a metadata listener bound on each box-network's gateway; per request,
  resolve caller source-IP → box via the engine/store (the store already knows
  box→InstanceRef; add box→IP). Endpoints: `GET /v1/me` (box metadata),
  `GET /v1/me/time`, `POST /v1/me/{keep-alive,auto-suspend,idle,wake-on}`. Ship
  `cmd/box-guest` (HTTP client, `$BOX_META`). Optionally migrate the *agent's*
  auth to network-position too (hub resolves agent source-IP → box), retiring the
  env token entirely.
- **Effort:** M. **Risk:** medium (identity model). **Deps:** none for the Docker
  version; trusted box network (no source-IP spoofing — docker-managed IPs hold).

### F3 — Heartbeat + idle detection
- **What:** the box reports activity (heartbeat + load); the control plane tracks
  `last_heartbeat_at` and decides "idle".
- **Why:** prerequisite for auto-suspend.
- **Design:** the agent periodically POSTs `/v1/me/heartbeat` (load avg, conn
  count) via the metadata channel (F2). "Idle" = no heartbeat-activity for
  `idle_timeout` **and** load < `load_threshold`. Evaluated by the reconciler,
  like a second `EvalLifetime`.
- **Effort:** M. **Deps:** F2.

### F4 — Auto-suspend + idle tuning
- **What:** suspend an idle box; resume on reconnect. Per-box `auto_suspend`,
  `idle_timeout_override`, `load_threshold_override`, `keep_alive_until`.
- **Why:** the economics — idle boxes cost ~nothing.
- **Design:** add `PhaseSuspended` and `Suspend(ref)/Resume(ref)` to
  `ports.Compute` (microVM: pause+snapshot / resume; docker: pause/unpause as an
  approximation). New `box.Box` fields (persisted via boxsqlite): `AutoSuspend`,
  `IdleTimeout`, `LoadThreshold`, `KeepAliveUntil`. Reconciler gains an
  `EvalSuspend` step; the front-door `Attach` resumes a suspended box before
  bridging. Owner controls via F2 (`keep-alive`, `auto-suspend on/off`, `idle`).
- **Effort:** M–L. **Risk:** medium. **Deps:** F3; real value on F1 (snapshot),
  crude on docker.

### F5 — Wake-on-traffic  *(the killer feature)*
- **What:** a suspended box auto-resumes when traffic hits configured ports
  (`tcp/22`, `icmp`, …) — idle-but-instantly-available.
- **Why:** persistent boxes that cost nothing until touched. The magic.
- **Design — two tiers:**
  - **Tier A (control-plane-mediated, easy):** the front door and gateway already
    mediate access. On a connection to a suspended box, *resume then bridge*. This
    covers the main case (`ssh` to a suspended box) with no packet plumbing —
    basically F4's resume-on-Attach, generalized to the gateway. **Do this first.**
  - **Tier B (general):** arbitrary ports not via the control plane — host packet
    watch (eBPF / iptables `NFQUEUE`) that resumes the box on a matching packet,
    per its `wake_on_traffic` spec. Hard; defer.
- **Effort:** M (Tier A) / L (Tier B). **Deps:** F4.

### F6 — Image cloning (CoW golden images)
- **What:** boxes spawn as copy-on-write clones of a prebuilt golden rootfs
  (`debian-default@build-…`) for sub-second boot.
- **Why:** fast spawn; reproducible bases.
- **Design:** a golden rootfs built once; each box boots a CoW overlay (qcow2
  backing file / overlayfs / zfs-btrfs snapshot). A small image build+register
  pipeline (rootfs equivalent of docker images). On docker this is just image
  layers (already CoW) — so F6 is mainly a microVM concern.
- **Effort:** M–L. **Deps:** F1.

### F7 — MCP server in the guest tool  *(AI-agent angle)*
- **What:** `box-guest mcp [--bind]` serves an MCP server exposing the lifecycle
  ops (info, keep-alive, suspend, …) as tools.
- **Why:** agents manage their own sandboxes — the agent-sandbox differentiator.
- **Design:** an `mcp` subcommand on `box-guest` wrapping the F2 metadata calls as
  MCP tools (stdio + optional HTTP bind).
- **Effort:** S–M. **Deps:** F2.

### F8 — Flavors catalog (+ rootfs, classes)
- **What:** named flavors/classes mapping to cpu/mem/**rootfs** (reference
  shows `class: standard, flavor: default, 2 vCPU · 1024 MiB · 2G rootfs`).
- **Why:** the resource/pricing model. Partially done (`box.Flavor` cpu/mem).
- **Design:** extend `box.Flavor` with `RootfsGB`; config-driven catalog (not
  just builtins); `:flavor` resolves to it; rootfs sizes the overlay (F6).
- **Effort:** M. **Deps:** rootfs needs F1.

### F9 — Minimal box init  *(microVM PID-1)*
- **What:** microVMs have no systemd; a tiny PID-1 sets up net, launches the
  agent/sshd, heartbeats, handles suspend/resume.
- **Why:** required to boot a microVM box.
- **Design:** make `hopbox-agent` runnable as PID-1 (mount essentials, configure
  net, serve), or a thin init that execs it. **Deps:** F1.

---

## Plan (phasing & dependencies)

```
Phase 0  MVP (now)            ship boxd as-is (ssh box@host, isolated, persistent)
Phase 1  Foundation/security  F2 metadata-identity + box-guest   ── Docker-compatible, kills token-in-env
         (no microVM yet)     F8 flavors catalog (cpu/mem parts)
Phase 2  The bet              F1 microVM provider (KVM)          ── unlocks everything below
Phase 3  Lifecycle (on F1)    F3 heartbeat/idle → F4 suspend → F5 wake (Tier A) → F6 clone → F9 init
Phase 4  AI                   F7 MCP-in-guest
Phase 5  Hardening            F5 Tier B (general wake-on-traffic)
```

**Why this order:** Phase 1 is Docker-compatible, low-risk, and a *security
upgrade* (token-in-env) — value before the big bet. Phase 2 (microVM) is the
moat and the precondition for the economic features. Phase 3 is the differentiated
product (suspend/wake). Don't start Phase 2+ until the MVP has validated demand.

## Effort / risk summary

| Feature | Effort | Risk | Gated on |
|---|---|---|---|
| F1 microVM provider | L | high | KVM |
| F2 metadata + box-guest | M | med | — (Docker ok) |
| F3 heartbeat/idle | M | low | F2 |
| F4 auto-suspend | M–L | med | F3 (+F1 for real) |
| F5 wake-on-traffic (A/B) | M / L | med/high | F4 |
| F6 image cloning | M–L | med | F1 |
| F7 MCP-in-guest | S–M | low | F2 |
| F8 flavors catalog | M | low | F1 for rootfs |
| F9 box init | M | med | F1 |

## Open decisions

- **VMM:** Firecracker (GPU out of scope, so no need for Cloud Hypervisor).
- **Agent identity:** migrate the reverse-dial auth to network-position (retire the
  env token) or keep the token and only add metadata for the guest tool?
- **Wake-on-traffic mechanism:** control-plane-mediated only, or invest in eBPF.
- **Scope:** boxd-only, or does hopbox (dev-env) inherit suspend/metadata too?
- **Image pipeline:** how golden rootfs images are built/registered/versioned.
- **Validate first:** is there pull for the box product before committing to F1?
