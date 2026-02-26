# hopbox-hostd Design

## Goal

A gRPC daemon that runs on each bare metal host, wrapping the silo VM library to expose workspace lifecycle operations. Replaces the manual poc-managed workflow with a single `CreateWorkspace` API call that handles the full provisioning flow.

## Architecture

```
Control Plane (future, Step 3)
       │
       ▼ gRPC (mTLS when control plane arrives)
┌─────────────────────────────┐
│  hopbox-hostd               │
│  ├─ gRPC server (:9090)     │
│  ├─ silo.Runtime            │
│  ├─ Provisioner             │
│  │  (inject, entropy, keys, │
│  │   iptables, hop-agent)   │
│  ├─ Port allocator          │
│  │  (51820-52820 range)     │
│  └─ Host status reporter    │
└─────────────────────────────┘
       │
       ▼ Firecracker + ZFS
     [ VMs ]
```

The daemon absorbs all poc-managed logic (agent injection, entropy seeding, WireGuard key exchange, iptables port forwarding) into the CreateWorkspace RPC.

## gRPC Service

```protobuf
service HostService {
  rpc CreateWorkspace(CreateWorkspaceRequest) returns (CreateWorkspaceResponse);
  rpc DestroyWorkspace(DestroyWorkspaceRequest) returns (DestroyWorkspaceResponse);
  rpc SuspendWorkspace(SuspendWorkspaceRequest) returns (SuspendWorkspaceResponse);
  rpc ResumeWorkspace(ResumeWorkspaceRequest) returns (ResumeWorkspaceResponse);
  rpc GetWorkspace(GetWorkspaceRequest) returns (GetWorkspaceResponse);
  rpc ListWorkspaces(ListWorkspacesRequest) returns (ListWorkspacesResponse);
  rpc HostStatus(HostStatusRequest) returns (HostStatusResponse);
}
```

**CreateWorkspace** does: create VM → start → inject hop-agent → seed entropy → exchange WireGuard keys → start hop-agent → set up port forwarding. Returns client config (endpoint, pubkey, allowed IPs, port).

**DestroyWorkspace** cleans up everything: kill Firecracker, remove ZFS dataset, delete TAP device, remove iptables rules, free port.

**HostStatus** reports capacity: total/available vCPUs, memory, disk, running/suspended workspace count.

No streaming RPCs — YAGNI. Add later if needed.

## Project Layout

```
hopbox/
├── proto/
│   └── hostd/v1/
│       └── hostd.proto
├── gen/
│   └── hostd/v1/
│       ├── hostd.pb.go
│       └── hostd_grpc.pb.go
├── cmd/
│   └── hopbox-hostd/
│       └── main.go
├── internal/
│   └── hostd/
│       ├── server.go        # gRPC implementation
│       ├── provisioner.go   # WireGuard inject/keys/iptables
│       └── portalloc.go     # UDP port allocator
```

`hopbox-hostd` imports silo as a Go module dependency. Proto tooling via `buf`.

## Auth

Localhost-only binding (127.0.0.1:9090) for initial development. mTLS added when building the control plane (Step 3).

## Port Allocator

Assigns UDP ports from range 51820-52820. Each workspace gets a unique host port mapped via iptables DNAT to its VM's WireGuard listener (always :51820 inside VM). Port assignments persisted in state so they survive daemon restarts.

## Provisioner

Moves poc-managed logic into `internal/hostd/provisioner.go`:

- **Agent injection**: HTTP server on TAP IP, VM curls binary, chmod +x
- **Entropy seeding**: perl RNDADDENTROPY ioctl to unblock Go's getrandom()
- **Key exchange**: hop-agent setup phase1 (server keys) → phase2 (client pubkey)
- **iptables**: DNAT + FORWARD rules inserted at chain top (-I FORWARD 1), removed on destroy

## Stability Fixes (vs PoC)

- **Entropy**: seeded automatically during CreateWorkspace
- **Cleanup**: DestroyWorkspace removes all resources (TAP, iptables, ZFS, port)
- **iptables**: managed per-workspace, inserted/removed cleanly
- **Version sync**: hostd and hop-agent built from same repo with same version tag
- **State reconciliation**: on startup, silo.Init() reconciles state DB, daemon re-verifies port forwarding for running VMs

## Daemon Lifecycle

- Runs as systemd service
- Graceful shutdown on SIGTERM: drains in-flight RPCs, does not touch running VMs
- VMs survive daemon restarts (silo state DB handles reconciliation)

## Testing

**Unit tests**: port allocator (alloc, release, exhaustion, persistence), provisioner helpers (IP math, config gen, key gen).

**Integration tests on VPS**: CreateWorkspace → verify VM + hop-agent + port forwarding. DestroyWorkspace → verify full cleanup. Suspend/Resume → verify WireGuard reconnects.

**Smoke test**: grpcurl from localhost to create workspace, hop up from laptop to connect.
