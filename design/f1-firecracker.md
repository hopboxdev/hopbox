# F1 — Firecracker microVM provider (scope)

Detailed scoping for the microVM compute provider. Goal: a `ports.Compute`
implementation backed by Firecracker, so a box is a **microVM** (hardware
isolated) instead of a container — the moat, and the substrate for the
suspend/snapshot/wake lifecycle (F3–F6).

## 0. Hard prerequisite — a KVM-capable host (go/no-go)

Firecracker needs **`/dev/kvm`** (Linux + hardware virtualization). A typical
cloud **VPS is itself a VM without nested virtualization**, so `/dev/kvm` is
absent and Firecracker cannot run. The reference product runs on OVH **bare
metal**.

**Decide the F1 host before anything else.** Confirm with:

```sh
ls -la /dev/kvm                      # must exist (and be RW for the runner)
egrep -c '(vmx|svm)' /proc/cpuinfo   # > 0 = CPU exposes virt extensions
lsmod | grep -E '^kvm'               # kvm + kvm_intel/kvm_amd loaded
systemd-detect-virt                  # 'none' (bare metal) ideal; a VM name ⇒ needs nested virt
```

> **Confirmed & PROVEN (2026-06): the existing hopbox VPS passes this gate.** It is
> a KVM guest with **nested virtualization enabled** — `/dev/kvm` present
> (`root:kvm`), 12 `vmx` flags, `kvm`/`kvm_intel` loaded, `systemd-detect-virt = kvm`.
> **Firecracker v1.14.1 is installed and a microVM was booted to a login prompt**
> (Linux 4.14 → systemd → `getty.target` → `login:`). So F1.0 is **GO** and F1 is
> **not** blocked on new hardware.
>
> Caveat: nested virt carries a performance penalty (a VM inside a VM). Fine for
> **building and validating** F1; for production scale consider bare metal later.
>
> The host is a clean slate: an earlier Firecracker PoC ("silo": a `zfs`-pool +
> `poc-managed`/`hopbox-hostd` daemons) was removed. F1 starts fresh — its own
> kernel, golden rootfs, networking, and `ports.Compute` provider.

## 1. Architecture (per box)

- A **Firecracker process** (run under the **jailer** for isolation), each with a
  private API unix socket.
- A **tap device** per VM on a host bridge (e.g. `boxnet`, `10.0.0.0/24`); the VM
  gets an IP. It reaches the agent hub and the metadata API via the bridge gateway
  — the *same* reverse-dial + metadata model we already have, just on a VM NIC.
- A **CoW rootfs overlay** cloned from a golden image (F6) + a pinned **kernel**
  (`vmlinux`).
- **Inside the VM:** a minimal init (F9) brings up networking and launches
  `hopbox-agent`, which reverse-dials the hub. **The agent + protocol are
  unchanged** — only the substrate differs. That's the payoff of the `ports.Compute`
  seam: `box.Engine`, the reconciler, the front door, the metadata API all stay put.

## 2. Mapping to `ports.Compute`

| Method | microVM behaviour |
|---|---|
| `Provision(req)` | alloc VM id; create+attach tap, assign IP; build CoW rootfs overlay; write FC config (kernel, rootfs drive, `vcpus`←CPUMillis, `mem`←MemMB, env via kernel cmdline or a config drive); boot via jailer. Return `Instance{Ref: vmID, IP, Phase: Running}`. |
| `Status(ref)` | query FC API `InstanceInfo` (or track the process) → `InstancePhase`. |
| `Stop(ref)` | FC `SendCtrlAltDel` / pause. |
| `Destroy(ref)` | kill FC, delete tap, delete overlay. |
| **`Suspend/Resume`** *(new, for F4)* | FC `CreateSnapshot` (pause→snapshot to disk) / `LoadSnapshot` (restore). The clean microVM suspend — add these two methods to `ports.Compute`. |

`Instance.IP` (added in F2) already carries the VM's IP into the metadata identity.

## 3. Components to build

1. **FC control** — `firecracker-microvm/firecracker-go-sdk` (pragmatic) or raw
   HTTP over the API socket.
2. **Networking** — tap create/attach/teardown (netlink), static IP via cmdline,
   the VM bridge; reuse the egress-fence concept on the VM subnet.
3. **Rootfs/image** — golden image build (F6) + per-box CoW overlay (overlayfs /
   qcow2 backing / dm-thin) + a pinned `vmlinux`.
4. **In-VM init (F9)** — `hopbox-agent` as PID-1 (mount essentials, net up, read
   env, run), or a tiny init that execs it.
5. **Provider package** — `providers/compute/microvm` behind `//go:build firecracker`,
   implementing `ports.Compute`, with a runtime KVM check.

## 4. Build / test strategy

- **Cannot build-test on macOS / non-KVM hosts.** Gate behind `//go:build firecracker`.
- **Unit-test the pure parts (no KVM):** FC config generation, vcpu/mem mapping,
  overlay/tap naming, kernel cmdline assembly.
- **Integration-test on a KVM Linux host:** boot a VM → agent connects →
  `box-guest info` works → suspend/restore round-trips. Needs a KVM CI runner or
  the chosen bare-metal host.

## 5. Increments

- **F1.0 — go/no-go:** secure a KVM host; pin a kernel + minimal golden rootfs;
  boot Firecracker by hand (no hopbox) to prove the host. *Cheap; do first.*
- **F1.1:** `providers/compute/microvm` skeleton — `ports.Compute` + FC config
  generation (unit-tested) + boot a static VM via the SDK (no net).
- **F1.2:** networking — tap + bridge + IP; the VM reaches the host (hub + metadata).
- **F1.3:** in-VM init (F9) launches the agent; a box reaches `Running`;
  `ssh box@host` works on microVMs end to end.
- **F1.4:** CoW overlay from a golden image (F6) for sub-second spawn.
- **F1.5:** boxd selects the provider (`--compute microvm`); reuse the fence +
  metadata on the VM bridge.
- *(later)* **F4:** `Suspend/Resume` via FC snapshot → then F5 wake-on-traffic.

## 6. Risks / unknowns

- **KVM host availability** — the gate (see §0).
- Networking model (tap/bridge/IP, fence on the VM subnet) — the fiddliest part.
- Golden-image + kernel pipeline (overlaps F6) — new build infra.
- Jailer / seccomp hardening — get isolation right (it's the whole point).
- Agent-as-init (F9) — boot reliability.
- Snapshot/restore correctness (clock, network, fd state) for F4.

## 7. Open decisions

- **Host:** which bare-metal/KVM box? (OVH dedicated; the VPS won't do.)
- SDK vs raw FC API.
- Rootfs CoW mechanism (overlayfs vs qcow2 vs dm-thin).
- Kernel: prebuilt vs self-built; version pin.
- Env injection: kernel cmdline vs config drive.
- Scope: microVM backend for `boxd` only first, or hopbox (dev-env) too?

## 8. Effort

**L (multi-week), highest risk / highest reward.** Sequence the increments;
F1.0 is a cheap go/no-go gate before committing real time. The `ports.Compute`
seam means the blast radius is contained to a new provider package — nothing in
`core/box`, the engine, the front door, or the metadata API changes.
