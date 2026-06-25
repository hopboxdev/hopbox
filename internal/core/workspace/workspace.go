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

// IngressPort is a desired exposed endpoint: a named port inside the workspace.
type IngressPort struct {
	Name string `json:"name"` // logical endpoint name (e.g. "app")
	Port int32  `json:"port"` // port the workspace service listens on
}

// Endpoint is an observed exposed endpoint, written by the reconciler after the
// Ingress provider resolves an IngressPort to a reachable address.
type Endpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`  // reachable address, e.g. https://app-w1.gw.host
	Port int32  `json:"port"` // the workspace port it targets
	Ref  string `json:"ref"`  // provider-opaque handle / gateway route key
}

// Workspace is Hopbox's single declarative resource (M1 subset + M3 ingress).
type Workspace struct {
	// metadata
	ID       string
	TenantID string
	Owner    string
	Name     string
	// spec (desired)
	ImageRef string
	Backend  string        // compute backend (e.g. docker|k8s); "" = auto, resolved via ResolveBackend
	MemMB    int64         // 0 = provider default
	Ingress  []IngressPort // desired exposed endpoints
	// lifetime (desired): an ephemeral workspace is reaped when its owner detaches.
	// Persistent (the default) leaves all fields zero and is never reaped.
	Ephemeral bool          // true = reap on disconnect (krillbox-style temporary box)
	Grace     time.Duration // stay-alive after the agent detaches; 0 = reap immediately
	MaxTTL    time.Duration // hard cap from CreatedAt regardless of connection; 0 = none
	Deadline  *time.Time    // reap-after instant, stamped on detach; nil while attached
	// status (observed, written by the reconciler / agenthub)
	Phase          Phase
	InstanceRef    string     // provider-opaque (docker container id)
	HomeMount      string     // host path of the persistent home
	BootstrapToken string     // one-time, workspace-scoped agent token
	AgentConnected bool       // set by agenthub when the agent dials in (box-alive signal)
	Attached       bool       // an owner SSH front-door session is held open (reap signal for ephemeral boxes)
	Endpoints      []Endpoint // resolved endpoints (one per Ingress, set when Running)
	Message        string     // last status / failure detail
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
