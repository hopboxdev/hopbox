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

// AgentImage describes how a Compute provider obtains and runs the mesa-agent
// inside a workspace. It replaces M1's host-path field (a Docker-only
// assumption that could not work for a Kubernetes pod). Each provider
// interprets it: docker pulls the image and copies the binary into a volume
// (or bind-mounts HostBinaryPath as a dev fast-path); kubernetes uses an
// initContainer from the image to seed a shared volume.
type AgentImage struct {
	ImageRef       string // OCI image carrying the agent binary
	BinaryPath     string // path of the binary inside that image
	TargetPath     string // where to place + run it in the workspace
	HostBinaryPath string // docker-only dev fast-path: bind-mount this host binary instead
}

// ProvisionRequest is the vendor-neutral spec the reconciler hands a Compute provider.
type ProvisionRequest struct {
	WorkspaceID string
	ImageRef    string
	MemMB       int64
	Mounts      []Mount
	Env         map[string]string // includes MESA_AGENT_TOKEN, MESA_CONTROL_ADDR
	Agent       AgentImage        // how to side-load the agent (replaces AgentPath)
}

type HomeRequest struct {
	WorkspaceID string
	TenantID    string
	Owner       string
}

// ExposeRequest asks an Ingress provider to make a workspace port reachable.
type ExposeRequest struct {
	WorkspaceID string
	Name        string // logical endpoint name within the workspace (e.g. "app")
	Port        int32  // port inside the workspace
	Scheme      string // subdomain | port-range | tcp-tunnel
	TenantID    string
}

// Endpoint is a reachable address for an exposed workspace port.
type Endpoint struct {
	Ref  string // provider-opaque handle (also the gateway route key)
	URL  string // reachable address, e.g. https://app-alice.gw.host
	Name string
	Port int32
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

// Ingress maps a workspace port onto a reachable Endpoint at the service gateway.
type Ingress interface {
	Expose(ctx context.Context, r ExposeRequest) (Endpoint, error)
	Unexpose(ctx context.Context, ref string) error
}
