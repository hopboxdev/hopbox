// Package ports defines Mesa's provider contracts as Go interfaces with
// vendor-neutral types. In M1 providers are in-process; M2 lifts these to
// protobuf + an out-of-process loader. No provider SDK type appears here.
package ports

import "context"

// ---- neutral types ----

type InstancePhase string

const (
	InstanceRunning InstancePhase = "running"
	InstanceStopped InstancePhase = "stopped"
	InstanceGone    InstancePhase = "gone"
	InstanceFailed  InstancePhase = "failed"
)

// Mount is how a Storage provider hands persistent data to a Compute provider.
type Mount struct {
	Source   string // provider-opaque (host path, volume name, PVC claim, ...)
	Target   string // path inside the workspace
	ReadOnly bool
}

// Instance is a provider-opaque handle to a running (or not) workspace box.
type Instance struct {
	Ref   string
	Phase InstancePhase
}

// ProvisionRequest is the vendor-neutral spec the reconciler hands a Compute provider.
type ProvisionRequest struct {
	WorkspaceID string
	ImageRef    string
	MemMB       int64
	Mounts      []Mount
	Env         map[string]string // includes MESA_AGENT_TOKEN, MESA_CONTROL_ADDR
	AgentPath   string            // host path to the mesa-agent binary to side-load
}

type HomeRequest struct {
	WorkspaceID string
	TenantID    string
	Owner       string
}

// ---- the contracts ----

type Compute interface {
	Provision(ctx context.Context, r ProvisionRequest) (Instance, error)
	Status(ctx context.Context, ref string) (Instance, error)
	Stop(ctx context.Context, ref string) error
	Destroy(ctx context.Context, ref string) error
}

type Storage interface {
	EnsureHome(ctx context.Context, r HomeRequest) (Mount, error)
	Delete(ctx context.Context, homeRef string) error
}
