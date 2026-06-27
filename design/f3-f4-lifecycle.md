# F3 + F4 — idle detection & auto-suspend (scope)

The economic lifecycle: an idle box **suspends** (costs ~nothing) and **wakes on
access**. This is the box product's moat, and it builds directly on the microVM
substrate now that F1/F2 are done. F3 detects idle; F4 suspends/resumes.

## What we already have (the substrate)

- **microVM provider** (`providers/compute/microvm`) — boots via `firecracker
  --no-api --config-file`, CoW rootfs, tap/IP, egress fence.
- **metadata API** (`internal/core/boxmeta`) — `GET /v1/me`,`/v1/me/time`,
  identify-by-source-IP. Currently **read-only** (a `Resolver` func).
- **agent** (`cmd/hopbox-agent`) — reverse-dials the hub, serves shells.
- **box model + reconciler** — `box.Box` has `Attached` (front-door session held)
  and `AgentConnected` (agent dialed in); `box.Reconciler` provisions/reaps and
  already does an `EvalLifetime` pass.
- **`ports.Compute`** — `Provision/Status/Stop/Destroy` (no Suspend/Resume yet).

---

## F3 — heartbeat + idle detection

- **What:** the box reports liveness/load; the control plane decides "idle".
- **Idle =** `!Attached` **and** load < threshold, sustained for `idle_timeout`.
  `Attached` already covers "someone's in the box"; the missing signal is **load**
  (background work with no session attached).
- **Design:**
  - `box.Box`: add `LastHeartbeat time.Time`, `Load float64` (persist in
    boxsqlite + the dev-env store, like the cpu_millis migration).
  - **metadata API gains a write path:** `POST /v1/me/heartbeat {load}` →
    `boxmeta` needs an *updater* alongside the `Resolver` (a
    `func(ctx, ip, Heartbeat) error` that read-modify-writes the box). Keep it
    box-clean.
  - **agent:** a heartbeat loop — every ~15s POST `/proc/loadavg` (1-min) to
    `$BOX_META/v1/me/heartbeat`. (No credential — source-IP identity.)
  - **reconciler:** `idle(b) = !b.Attached && b.Load < threshold && now-b.LastActive >
    idle_timeout`, where `LastActive = max(detach time, last high-load heartbeat)`.
- **Effort:** M. **Deps:** F2 (metadata) ✅. No microVM dependency — works for
  docker boxes too.

## F4 — auto-suspend + resume (+ idle tuning)

- **What:** suspend an idle box to disk; resume it on access. Per-box
  `auto_suspend`, `idle_timeout`, `load_threshold`, `keep_alive_until`.
- **The crux — Firecracker snapshots require the API socket.** The provider
  currently boots `--no-api --config-file`; snapshots (`PUT /snapshot/create`,
  `PUT /snapshot/load`) need the **API socket**. So F4's prerequisite is
  **migrating the boot path to `--api-sock`** (PUT boot-source/drives/machine-
  config/network-interfaces + `InstanceStart`). The `fcConfig` shapes already
  generated are reused as the API request bodies — contained, but it touches the
  proven start path. **This is the main risk.**
- **Design:**
  - `ports.Compute`: add `Suspend(ctx, ref)` / `Resume(ctx, ref)`.
    - microVM: Suspend = `PUT /vm {Paused}` + `PUT /snapshot/create` (state+mem
      files) + kill FC. Resume = new FC + `PUT /snapshot/load {resume_vm:true}`.
      **Hold the tap + IP across suspend** (recreate the same `fctap<octet>` on
      resume; the IP lives in the snapshot's kernel state). Don't `freeIP` on
      suspend.
    - docker: pause/unpause (a crude approximation; the real value is microVM).
  - `box.Box`: `PhaseSuspended` + `AutoSuspend bool`, `IdleTimeout`,
    `LoadThreshold`, `KeepAliveUntil` (persisted).
  - **reconciler:** an `EvalSuspend` pass — `idle(b) && b.AutoSuspend &&
    now > b.KeepAliveUntil` ⇒ `Suspend`. **Resume on `Attach`** (the front door
    resumes a suspended box before bridging — this is also F5 Tier A,
    wake-on-access).
  - **suspend ≠ dead:** when a box suspends, its agent's hub session drops →
    `AgentConnected=false`. The reconciler must read this as *suspended*, not
    *crashed* (don't re-provision). Gate on `Phase`.
  - **box-guest** gains the krillbox owner commands → metadata write endpoints:
    `keep-alive [dur]`, `auto-suspend on|off`, `idle [--timeout][--load]`.
- **Effort:** L. **Deps:** F3 + the API-socket migration. Real value on microVM
  (clean snapshot/restore); docker is a fallback.

## Increments

```
F3.1  metadata write path + box heartbeat fields + agent heartbeat loop
F3.2  reconciler idle() computation
F4.1  provider: migrate boot to the FC API socket   (prerequisite; the risk)
F4.2  provider Suspend/Resume via FC snapshot/restore + ports.Compute methods
F4.3  box suspend fields + reconciler EvalSuspend + resume-on-Attach
F4.4  box-guest lifecycle commands + metadata write endpoints
F4.5  docker Suspend/Resume (pause/unpause) — the fallback
```

Verification stays host-driven (deploy boxd → idle a box → watch it suspend →
ssh again → watch it resume), as with F1.

## Risks / open decisions

- **FC API-socket migration (F4.1)** — touches the proven `--no-api` path; the
  biggest risk. Mitigate: keep `--no-api` for the non-snapshot path until F4.2
  works, or switch wholesale and re-run the F1.5 e2e.
- **Clock drift on resume** — a restored VM's wall clock is stale. FC handles some
  of this; the box may need a resume hook to re-sync (the metadata `/v1/me/time`
  exists for exactly this).
- **Snapshot disk cost** — each suspended box dumps its RAM (e.g. 512 MB) to disk.
  Fine (disk is cheap), but bounds how many suspend concurrently; consider a cap.
- **Idle defaults** — `idle_timeout` (e.g. 15m), `load_threshold` (e.g. 0.2).
- **Wake-on-traffic scope** — F4 delivers wake-on-*access* (resume-on-Attach,
  F5 Tier A). General wake-on-arbitrary-port (F5 Tier B, eBPF) stays deferred.
- **dev-env scope** — do Workspaces inherit suspend, or boxd-only first?

## Effort

**L overall.** F3 is a clean M (no microVM dep). F4 is the L — gated on the
API-socket migration, then the snapshot mechanics + the reconciler state machine.
The `ports.Compute` seam keeps it a provider change; `box.Engine`/front door gain
only the resume-on-Attach hook.
