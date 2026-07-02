// Package box is hopbox's compute-box core — the primitive a user reaches with
// `ssh box@host`. It owns the box request grammar, backend selection,
// lifetime/flavor, the box model, and the engine.
//
// Boundary rule: box MUST NOT import a provider SDK or any would-be dev-env layer
// (the dev-env that builds on top of the substrate lives in a separate repo). The
// dependency points only inward, so the substrate compiles and ships on its own.
// See internal/core/boundary_test.go.
package box

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Box is hopbox's core compute unit: a container/microVM reached over the agent
// reverse-connection, driven declaratively from spec to running. The dev-env
// layer (workspace) embeds this and adds a persistent home + gateway endpoints.
type Box struct {
	// metadata
	ID       string
	TenantID string
	Owner    string // opaque principal (key fingerprint, login id, …) from the Authenticator
	Name     string
	// spec (desired)
	ImageRef  string
	Backend   string // compute backend (docker|kubernetes|…); "" = auto, resolved via ResolveBackend
	MemMB     int64  // memory cap (MiB); 0 = provider default
	CPUMillis int64  // CPU cap in milli-cores (1000 = 1 vCPU); 0 = unlimited
	// lifetime (desired): an ephemeral box is reaped when its owner detaches.
	// Persistent (the default) leaves these zero and is never reaped.
	Ephemeral bool          // true = reap on disconnect (temporary box)
	Grace     time.Duration // stay-alive after the agent detaches; 0 = reap immediately
	MaxTTL    time.Duration // hard cap from CreatedAt regardless of connection; 0 = none
	Deadline  *time.Time    // reap-after instant, stamped on detach; nil while attached
	// suspend (persistent boxes only): snapshot to disk when idle, wake on attach.
	AutoSuspend         bool          // true = suspend when idle (vs reap); persistent boxes
	KeepAliveUntil      time.Time     // pin: do not suspend before this instant (box-guest keep-alive)
	IdleTimeoutOverride time.Duration // per-box idle timeout; 0 = use the daemon default
	// status (observed, written by the reconciler / agenthub)
	Phase          Phase
	InstanceRef    string // provider-opaque (e.g. docker container id)
	IP             string // box IP on its network; identifies it to the metadata API by source IP
	BootstrapToken string // one-time, box-scoped agent token
	AgentConnected bool   // set by agenthub when the agent dials in (box-alive signal)
	Attached       bool   // an owner SSH front-door session is held open (reap signal)
	Message        string // last status / failure detail
	// activity (observed, written by the metadata heartbeat) — drives idle/suspend.
	Load       float64   // last reported load average
	LastActive time.Time // last time the box was busy (attached or load over threshold)
	// self-report: the in-box agent tells the control plane what it's doing (box-guest
	// status). Drives the fleet at-a-glance view; not interpreted by the reconciler.
	AgentState  string // working | blocked | done | ""
	AgentStatus string // free-form status line ("compiling 3/5", "waiting on approval")
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// New returns a Pending Box with fresh id and timestamps.
func New(tenantID, owner, name, imageRef string) *Box {
	now := time.Now().UTC()
	return &Box{
		ID:        newID(),
		TenantID:  tenantID,
		Owner:     owner,
		Name:      name,
		ImageRef:  imageRef,
		Phase:     PhasePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
