# Hopbox

**A vendor-neutral, self-hostable control plane for development environments.**

Run one control plane; give users isolated, persistent, reproducible dev
workspaces on whatever compute you plug in. The *same* binary serves a solo
developer (SQLite + Docker) and a platform team (Kubernetes, a wildcard-domain
gateway, metering) — the difference is configuration and which **providers** are
loaded, not a fork.

> Hopbox is a greenfield rewrite of the original `hopbox` SSH gateway. The good
> parts of the UX are kept; the seams are now the architecture.

## How it works

Three planes:

- **Control plane (`hopboxd`)** — an API + a declarative reconciler (observe →
  diff → act) + state store (SQLite). It models one resource, the `Workspace`,
  and drives it toward its spec.
- **Provider plane** — six versioned protobuf contracts, each loadable in-process
  or over remote gRPC: **Compute** (docker, kubernetes), **Storage** (localfs,
  k8s PVC), **Ingress** (subdomain), **Identity** (static), **Build** (prebuilt),
  **Metering** (static-quota). No provider SDK type ever enters the core
  (enforced by an import-boundary lint).
- **Data plane** — each workspace runs `hopbox-agent`, a small static binary that
  **dials out** to the control plane and holds one multiplexed connection. The
  control plane never needs a route *into* a workspace, so a workspace can live
  behind NAT, in a private cluster, or another cloud and still be fully
  reachable. `hopbox shell` / `hopbox exec` and the gateway all ride that tunnel.

The stateless gateway (`hopbox-gw`) terminates HTTPS and forwards each request
into the target workspace over its agent connection, routing by Host — one
wildcard cert (`*.gw.example.com`) gives **unlimited** per-workspace endpoints.

## Quickstart (Docker)

```sh
make build                     # -> bin/hopboxd, hopbox, hopbox-gw, hopbox-agent-linux-*

# control plane (needs Docker)
bin/hopboxd --agent-bin ./bin/hopbox-agent-linux-amd64

# create a workspace, run a command in it, open a shell
bin/hopbox create demo --image ubuntu:24.04
bin/hopbox exec demo -- uname -a
bin/hopbox shell demo
```

Expose a workspace port at the gateway:

```sh
bin/hopbox create web --image python:3-slim --expose app:8000
bin/hopbox exec web -- sh -c 'cd /srv && python3 -m http.server 8000 &'
# -> https://app-<id>.gw.<your-domain>
```

## Components

| Binary | Role |
|--------|------|
| `hopboxd` | control plane: API, reconciler, agent hub, ingress route table, gateway tunnel |
| `hopbox` | user CLI: create / ls / rm / shell / exec |
| `hopbox-gw` | stateless service gateway (HTTPS, Host-routed) |
| `hopbox-agent` | injected into every workspace; reverse-dials the control plane |
| `hopbox-provider` | serve a provider over remote gRPC |

## Status

Early. The walking skeleton (Docker compute + agent), the Kubernetes providers,
all six provider contracts with reference implementations + conformance
batteries, the ingress gateway with TLS, and `shell`/`exec` work today.
Identity-backed API auth, build providers (devcontainer/nix), and HA (Postgres)
are on the roadmap.

## Building

Go 1.25+. Protos are managed with [`buf`](https://buf.build); regenerate with
`buf generate`. The Kubernetes providers are behind `-tags k8s`, the Docker
provider behind `-tags docker`; both have conformance suites that run offline.
