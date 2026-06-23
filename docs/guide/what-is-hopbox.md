# What is Hopbox

Hopbox is a **self-hosted, vendor-neutral control plane for development
environments**. You run one control plane (`hopboxd`); it gives you persistent,
reproducible **workspaces** — containers on Docker or pods on Kubernetes — that
you reach from anywhere over native SSH or host-routed HTTPS.

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
  defaults (Docker, local FS, subdomain ingress, static identity) or bring your
  own; the core never imports a vendor SDK.

## What you get

- `hopbox create` a workspace from any OCI image, on Docker or Kubernetes.
- Native **SSH** to it (and VS Code, scp, rsync) via short-lived certificates.
- Host-routed **HTTPS** for web apps — every workspace port gets a URL.
- **Multi-user** isolation: each person owns their boxes and keys; auth via
  static tokens or OIDC SSO.

## Next

- [Quickstart](/guide/quickstart) — running in a few minutes.
- [SSH & VS Code](/guide/ssh) · [Auth & multi-user](/guide/auth)

::: info Status
Hopbox is at v0.1. The architecture and access model documented here are stable;
build providers beyond `prebuilt` and additional ingress schemes (raw TCP
port-forwarding) are in progress and will be documented as they land.
:::
