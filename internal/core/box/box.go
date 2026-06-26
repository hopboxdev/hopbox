// Package box is hopbox's standalone compute-box core — the primitive a user
// reaches with `ssh box@host`. It owns the box request grammar, backend
// selection, lifetime/flavor, the box model, and (incrementally) the engine.
//
// Boundary rule: box MUST NOT import the dev-environment layer (core/workspace,
// api, gateway, identity). The dependency points only inward — workspace and the
// dev-env build on top of box — so the box product can be compiled and shipped
// without any of them. Keep `go list -deps ./internal/core/box` free of those
// packages.
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
	// status (observed, written by the reconciler / agenthub)
	Phase          Phase
	InstanceRef    string // provider-opaque (e.g. docker container id)
	BootstrapToken string // one-time, box-scoped agent token
	AgentConnected bool   // set by agenthub when the agent dials in (box-alive signal)
	Attached       bool   // an owner SSH front-door session is held open (reap signal)
	Message        string // last status / failure detail
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
