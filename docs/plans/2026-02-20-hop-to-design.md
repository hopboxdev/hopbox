# hop to — Design Doc

**Date:** 2026-02-20

## Purpose

`hop to <target>` migrates a workspace from the current host to a new one in a
single command. It snapshots the source, bootstraps the target via SSH, restores
the snapshot over a temporary WireGuard tunnel, and switches the default host.

## Use case

You have a workspace running on a VPS — services, data, config, everything
accumulated over time. You want to move it to a different host (different
provider, closer datacenter, better specs). Without `hop to`, this means
reinstalling packages, recreating services, and manually copying state. `hop to`
makes it one command.

## Command interface

```
hop to <target-name> --host <ip> [--user root] [--key ~/.ssh/id_ed25519] [--port 22]
```

Same SSH flags as `hop setup`. `target-name` is the name to register the new
host under. Source host is resolved the normal way (global flag → hopbox.yaml →
default host).

Confirmation prompt before any work begins:

```
Migrate workspace from mybox → newbox (203.0.113.2)?
  1. Create snapshot on mybox
  2. Bootstrap newbox via SSH
  3. Restore snapshot on newbox
  4. Set newbox as default host

Proceed? [y/N]
```

## Execution flow

```
Step 1/4  Snapshot    snap.create on source via existing tunnel state proxy
Step 2/4  Bootstrap   Bootstrap() inline — TOFU, install agent, key exchange
Step 3/4  Restore     temp WireGuard tunnel → probe agent → snap.restore → close tunnel
Step 4/4  Switch      hostconfig.SetDefaultHost(target)
```

On completion: `"Migration complete. Run 'hop up' to connect."`

### Error behaviour

- If snap.create fails → abort, nothing touched on target.
- If bootstrap fails → abort, snapshot exists on source, no partial state on target.
- If snap.restore fails → print recovery hint:
  `hop snap restore <id> --host <target>` so the user can retry manually.

### Service verification

Not part of `hop to`. The snapshot restores data files; starting services
requires `workspace.sync` which is `hop up`'s job. After restore we only verify
the agent health endpoint responds.

## Temporary tunnel pattern

After Bootstrap() returns the target HostConfig, spin up a transient
WireGuard client tunnel to reach the agent at `10.10.0.2:4200`:

```go
tunCfg, _ := targetCfg.ToTunnelConfig()
tun := tunnel.NewClientTunnel(tunCfg)

tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
defer tunCancel()
go tun.Start(tunCtx)

agentClient := &http.Client{
    Timeout:   agentClientTimeout,
    Transport: &http.Transport{DialContext: tun.DialContext},
}

agentURL := fmt.Sprintf("http://%s:%d/health", targetCfg.AgentIP, tunnel.AgentAPIPort)
if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
    return fmt.Errorf("target agent unreachable after bootstrap: %w", err)
}

_, err = rpcclient.CallVia(agentClient, target, "snap.restore", map[string]string{"id": snapID})
// tunCancel() deferred — tunnel tears down automatically
```

The 5-minute tunnel context is generous; restore should complete well within that.

## Code changes

**`cmd/hop/to.go`** — rewrite:
- Add SSH flags matching `SetupCmd`
- Confirmation prompt
- Sequential 4-step flow

**`cmd/hop/up.go`** → **`cmd/hop/agent.go`** — extract `probeAgent`:
- Move `probeAgent` out of `up.go` into a new `cmd/hop/agent.go` helper
  so both `up.go` and `to.go` can call it

No changes to internal packages. No new dependencies.
