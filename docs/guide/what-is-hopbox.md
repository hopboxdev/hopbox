# What is Hopbox

Hopbox is **two layers built on one box core**:

1. **[boxd](/guide/boxd)** — the compute layer. A single daemon that turns a
   host into a fleet of **compute boxes you reach over plain SSH**:
   `ssh box@host` spawns a Firecracker microVM and drops you in. Your SSH key is
   your identity, the username is the box spec — no signup, no client.
2. **hopbox** (this control plane, `hopboxd`) — a **self-hosted, vendor-neutral
   control plane for development environments** built on that same box core. It
   adds persistent storage-homes, per-workspace HTTPS ingress, accounts and
   identity, a gRPC API + the `hopbox` CLI, and pluggable compute (Docker,
   Kubernetes, or microVM).

> boxd is the compute layer; hopbox is a full dev-env that uses it.

The rest of this page is about the **hopbox** dev-env. If you just want
`ssh box@host` boxes on a server, jump to **[boxd](/guide/boxd)**.

You run one control plane (`hopboxd`); it gives you persistent, reproducible
**workspaces** — containers on Docker, pods on Kubernetes, or Firecracker
microVMs — that you reach from anywhere over native SSH or host-routed HTTPS.

## The model

```
  hopbox (CLI / VS Code)
        │  gRPC
        ▼
  ┌───────────────┐        reverse dial        ┌──────────────┐
  │   hopboxd     │  ◀───────────────────────  │  workspace   │
  │  control plane│                            │   + agent    │
  └───────────────┘                            └──────────────┘
   store · reconciler · agent hub · gateway
```

- **Workspaces dial out.** The agent inside each workspace opens a connection
  *to* `hopboxd` and keeps it. The control plane never routes *into* compute — so
  workspaces work behind NAT, inside a private cluster, anywhere with outbound
  network. SSH and HTTPS both ride that one reverse connection.
- **Declarative core.** A reconciler drives each workspace from its observed
  status toward its desired spec and heals drift — the Kubernetes controller
  *pattern*, with no Kubernetes dependency. State lives in a store.
- **Swappable providers.** Six versioned contracts — **Compute, Storage,
  Ingress, Identity, Build, Metering** — each with conformance tests. Ship the
  defaults (Docker, local FS, subdomain ingress, static identity), pick the
  Kubernetes or Firecracker **microVM** compute backend, or bring your own; the
  core never imports a vendor SDK.

## What you get

- `hopbox create` a workspace from any OCI image, on Docker, Kubernetes, or a
  Firecracker microVM.
- Native **SSH** to it (and VS Code, scp, rsync) via short-lived certificates.
- Host-routed **HTTPS** for web apps — every workspace port gets a URL.
- **Multi-user** isolation: each person owns their boxes and keys; auth via
  static tokens or OIDC SSO.

## Next

- [boxd — compute over SSH](/guide/boxd) — the standalone `ssh box@host` layer.
- [Quickstart](/guide/quickstart) — running in a few minutes.
- [SSH & VS Code](/guide/ssh) · [Auth & multi-user](/guide/auth)

::: info Status
Hopbox is at v0.1. The architecture and access model documented here are stable;
build providers beyond `prebuilt` and additional ingress schemes (raw TCP
port-forwarding) are in progress and will be documented as they land.
:::
