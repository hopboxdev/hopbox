// Package ports defines Hopbox's provider contracts as Go interfaces with
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
	IP    string // box IP on its network (for metadata-by-source-IP identity); "" if unknown
}

// AgentImage describes how a Compute provider obtains and runs the hopbox-agent
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
	CPUMillis   int64  // CPU cap in milli-cores (1000 = 1 vCPU); 0 = unlimited
	GuestBin    string // host path of the box-guest binary to side-load (read-only); "" = none
	Mounts      []Mount
	Env         map[string]string // includes HOPBOX_AGENT_TOKEN, HOPBOX_CONTROL_ADDR
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

// Suspender is an optional Compute capability: snapshot a box to disk (freeing
// CPU/RAM) and restore it on demand. Providers that can (microVM via Firecracker
// snapshots) implement it; callers type-assert. The box keeps its instance ref,
// IP, and storage across a suspend.
type Suspender interface {
	Suspend(ctx context.Context, ref string) error
	Resume(ctx context.Context, ref string) error
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

// Credential is what an Identity provider authenticates (api-key | oidc-token).
type Credential struct {
	Scheme string
	Value  string
}

// Principal is an authenticated identity. It carries TenantID — the seam a
// hyperscaler uses to map their customer model onto Hopbox tenants.
type Principal struct {
	ID          string
	TenantID    string
	DisplayName string
	Roles       []string // coarse RBAC: owner | tenant-admin | system
}

// AccessRequest asks whether a Principal may perform an action on a resource.
type AccessRequest struct {
	Principal Principal
	Action    string
	Resource  string
}

// Decision is the authorization outcome.
type Decision struct {
	Allow  bool
	Reason string
}

// Identity authenticates credentials to principals and authorizes actions.
type Identity interface {
	Authenticate(ctx context.Context, c Credential) (Principal, error)
	Authorize(ctx context.Context, r AccessRequest) (Decision, error)
}

// UsageEvent is a single metered fact about workspace usage.
type UsageEvent struct {
	TenantID    string
	PrincipalID string
	WorkspaceID string
	Kind        string // workspace.start | workspace.stop | cpu_seconds | storage_mb | ...
	Value       int64  // magnitude for metered kinds
	UnixMillis  int64  // event time
}

// PrincipalRef identifies whose quota to check.
type PrincipalRef struct {
	PrincipalID string
	TenantID    string
}

// QuotaState is the pre-flight provisioning gate for a principal.
type QuotaState struct {
	Allowed         bool
	WorkspacesUsed  int64
	WorkspacesLimit int64
	Reason          string
}

// Metering records usage and gates provisioning. Quota is a pre-flight check the
// reconciler runs before Provision; Emit records a usage event.
type Metering interface {
	Emit(ctx context.Context, e UsageEvent) error
	Quota(ctx context.Context, r PrincipalRef) (QuotaState, error)
}

// BuildRequest asks a Build provider to resolve a workspace source to an image.
type BuildRequest struct {
	WorkspaceID string
	SourceRef   string // git URL | path | image ref (for prebuilt)
	Provider    string // devcontainer | dockerfile | nix | prebuilt
	Options     map[string]string
	TenantID    string
}

// ImageRef is a resolved OCI image reference (the Build output). BuildRef is a
// handle to poll Status for async builds ("" when resolution was synchronous).
type ImageRef struct {
	Ref      string
	BuildRef string
}

// BuildStatus reports an async build's progress.
type BuildStatus struct {
	Phase    string // pending | building | ready | failed
	ImageRef string
	Message  string
}

// Build resolves a workspace source to an OCI image ref. The output is always an
// image ref, decoupling build from compute.
type Build interface {
	Resolve(ctx context.Context, r BuildRequest) (ImageRef, error)
	Status(ctx context.Context, buildRef string) (BuildStatus, error)
}
