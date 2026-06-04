package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseRunning      Phase = "Running"
	PhaseStopped      Phase = "Stopped"
	PhaseFailed       Phase = "Failed"
	PhaseDestroying   Phase = "Destroying"
)

// Workspace is Mesa's single declarative resource (M1 subset).
type Workspace struct {
	// metadata
	ID       string
	TenantID string
	Owner    string
	Name     string
	// spec (desired)
	ImageRef string
	MemMB    int64 // 0 = provider default
	// status (observed, written by the reconciler / agenthub)
	Phase          Phase
	InstanceRef    string // provider-opaque (docker container id)
	HomeMount      string // host path of the persistent home
	BootstrapToken string // one-time, workspace-scoped agent token
	AgentConnected bool   // set by agenthub when the agent dials in
	Message        string // last status / failure detail
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func New(tenantID, owner, name, imageRef string) *Workspace {
	now := time.Now().UTC()
	return &Workspace{
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

var transitions = map[Phase]map[Phase]bool{
	PhasePending:      {PhaseProvisioning: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseProvisioning: {PhaseRunning: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseRunning:      {PhaseProvisioning: true, PhaseStopped: true, PhaseFailed: true, PhaseDestroying: true},
	PhaseStopped:      {PhaseProvisioning: true, PhaseDestroying: true},
	PhaseFailed:       {PhaseProvisioning: true, PhaseDestroying: true},
	PhaseDestroying:   {},
}

// CanTransition reports whether from->to is a legal phase change.
func CanTransition(from, to Phase) bool {
	if from == to {
		return true
	}
	return transitions[from][to]
}
