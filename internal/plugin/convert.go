// Package plugin is the provider-plane adapter + loader. It is the ONLY place
// where generated protobuf types and the hand-written internal/core/ports
// types meet. The core never imports the generated wire package.
package plugin

import (
	pb "github.com/hopboxdev/hopbox/gen/hopbox/provider/v1"
	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// Converters are EXPORTED because the sibling internal/plugin/server package
// (the gRPC server adapter) also calls them.

func ToProtoPhase(p ports.InstancePhase) pb.Phase {
	switch p {
	case ports.InstanceRunning:
		return pb.Phase_RUNNING
	case ports.InstanceStopped:
		return pb.Phase_STOPPED
	case ports.InstanceGone:
		return pb.Phase_GONE
	case ports.InstanceFailed:
		return pb.Phase_FAILED
	default:
		return pb.Phase_PHASE_UNSPECIFIED
	}
}

func FromProtoPhase(p pb.Phase) ports.InstancePhase {
	switch p {
	case pb.Phase_RUNNING:
		return ports.InstanceRunning
	case pb.Phase_STOPPED:
		return ports.InstanceStopped
	case pb.Phase_GONE:
		return ports.InstanceGone
	case pb.Phase_FAILED:
		return ports.InstanceFailed
	default:
		return ports.InstanceFailed
	}
}

func ToProtoMount(m ports.Mount) *pb.Mount {
	return &pb.Mount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}
}
func FromProtoMount(m *pb.Mount) ports.Mount {
	if m == nil {
		return ports.Mount{}
	}
	return ports.Mount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}
}

func ToProtoAgent(a ports.AgentImage) *pb.AgentImage {
	return &pb.AgentImage{ImageRef: a.ImageRef, BinaryPath: a.BinaryPath, TargetPath: a.TargetPath, HostBinaryPath: a.HostBinaryPath}
}
func FromProtoAgent(a *pb.AgentImage) ports.AgentImage {
	if a == nil {
		return ports.AgentImage{}
	}
	return ports.AgentImage{ImageRef: a.ImageRef, BinaryPath: a.BinaryPath, TargetPath: a.TargetPath, HostBinaryPath: a.HostBinaryPath}
}

func ToProtoProvisionRequest(r ports.ProvisionRequest) *pb.ProvisionRequest {
	out := &pb.ProvisionRequest{
		WorkspaceId: r.WorkspaceID,
		ImageRef:    r.ImageRef,
		MemMb:       r.MemMB,
		Env:         r.Env,
		Agent:       ToProtoAgent(r.Agent),
	}
	for _, m := range r.Mounts {
		out.Mounts = append(out.Mounts, ToProtoMount(m))
	}
	return out
}
func FromProtoProvisionRequest(r *pb.ProvisionRequest) ports.ProvisionRequest {
	if r == nil {
		return ports.ProvisionRequest{}
	}
	out := ports.ProvisionRequest{
		WorkspaceID: r.WorkspaceId,
		ImageRef:    r.ImageRef,
		MemMB:       r.MemMb,
		Env:         r.Env,
		Agent:       FromProtoAgent(r.Agent),
	}
	for _, m := range r.Mounts {
		out.Mounts = append(out.Mounts, FromProtoMount(m))
	}
	return out
}

func ToProtoInstance(i ports.Instance) *pb.Instance {
	return &pb.Instance{Ref: i.Ref, Phase: ToProtoPhase(i.Phase)}
}
func FromProtoInstance(i *pb.Instance) ports.Instance {
	if i == nil {
		return ports.Instance{}
	}
	return ports.Instance{Ref: i.Ref, Phase: FromProtoPhase(i.Phase)}
}

func ToProtoHomeRequest(h ports.HomeRequest) *pb.HomeRequest {
	return &pb.HomeRequest{WorkspaceId: h.WorkspaceID, TenantId: h.TenantID, Owner: h.Owner}
}
func FromProtoHomeRequest(h *pb.HomeRequest) ports.HomeRequest {
	if h == nil {
		return ports.HomeRequest{}
	}
	return ports.HomeRequest{WorkspaceID: h.WorkspaceId, TenantID: h.TenantId, Owner: h.Owner}
}

func ToProtoExposeRequest(r ports.ExposeRequest) *pb.ExposeRequest {
	return &pb.ExposeRequest{WorkspaceId: r.WorkspaceID, Name: r.Name, Port: r.Port, Scheme: r.Scheme, TenantId: r.TenantID}
}
func FromProtoExposeRequest(r *pb.ExposeRequest) ports.ExposeRequest {
	if r == nil {
		return ports.ExposeRequest{}
	}
	return ports.ExposeRequest{WorkspaceID: r.WorkspaceId, Name: r.Name, Port: r.Port, Scheme: r.Scheme, TenantID: r.TenantId}
}

func ToProtoEndpoint(e ports.Endpoint) *pb.Endpoint {
	return &pb.Endpoint{Ref: e.Ref, Url: e.URL, Name: e.Name, Port: e.Port}
}
func FromProtoEndpoint(e *pb.Endpoint) ports.Endpoint {
	if e == nil {
		return ports.Endpoint{}
	}
	return ports.Endpoint{Ref: e.Ref, URL: e.Url, Name: e.Name, Port: e.Port}
}

func ToProtoCredential(c ports.Credential) *pb.Credential {
	return &pb.Credential{Scheme: c.Scheme, Value: c.Value}
}
func FromProtoCredential(c *pb.Credential) ports.Credential {
	if c == nil {
		return ports.Credential{}
	}
	return ports.Credential{Scheme: c.Scheme, Value: c.Value}
}

func ToProtoPrincipal(p ports.Principal) *pb.Principal {
	return &pb.Principal{Id: p.ID, TenantId: p.TenantID, DisplayName: p.DisplayName, Roles: p.Roles}
}
func FromProtoPrincipal(p *pb.Principal) ports.Principal {
	if p == nil {
		return ports.Principal{}
	}
	return ports.Principal{ID: p.Id, TenantID: p.TenantId, DisplayName: p.DisplayName, Roles: p.Roles}
}

func ToProtoAccessRequest(r ports.AccessRequest) *pb.AccessRequest {
	return &pb.AccessRequest{Principal: ToProtoPrincipal(r.Principal), Action: r.Action, Resource: r.Resource}
}
func FromProtoAccessRequest(r *pb.AccessRequest) ports.AccessRequest {
	if r == nil {
		return ports.AccessRequest{}
	}
	return ports.AccessRequest{Principal: FromProtoPrincipal(r.Principal), Action: r.Action, Resource: r.Resource}
}

func ToProtoDecision(d ports.Decision) *pb.Decision {
	return &pb.Decision{Allow: d.Allow, Reason: d.Reason}
}
func FromProtoDecision(d *pb.Decision) ports.Decision {
	if d == nil {
		return ports.Decision{}
	}
	return ports.Decision{Allow: d.Allow, Reason: d.Reason}
}

func ToProtoUsageEvent(e ports.UsageEvent) *pb.UsageEvent {
	return &pb.UsageEvent{
		TenantId: e.TenantID, PrincipalId: e.PrincipalID, WorkspaceId: e.WorkspaceID,
		Kind: e.Kind, Value: e.Value, UnixMillis: e.UnixMillis,
	}
}
func FromProtoUsageEvent(e *pb.UsageEvent) ports.UsageEvent {
	if e == nil {
		return ports.UsageEvent{}
	}
	return ports.UsageEvent{
		TenantID: e.TenantId, PrincipalID: e.PrincipalId, WorkspaceID: e.WorkspaceId,
		Kind: e.Kind, Value: e.Value, UnixMillis: e.UnixMillis,
	}
}

func ToProtoPrincipalRef(r ports.PrincipalRef) *pb.PrincipalRef {
	return &pb.PrincipalRef{PrincipalId: r.PrincipalID, TenantId: r.TenantID}
}
func FromProtoPrincipalRef(r *pb.PrincipalRef) ports.PrincipalRef {
	if r == nil {
		return ports.PrincipalRef{}
	}
	return ports.PrincipalRef{PrincipalID: r.PrincipalId, TenantID: r.TenantId}
}

func ToProtoQuotaState(q ports.QuotaState) *pb.QuotaState {
	return &pb.QuotaState{Allowed: q.Allowed, WorkspacesUsed: q.WorkspacesUsed, WorkspacesLimit: q.WorkspacesLimit, Reason: q.Reason}
}
func FromProtoQuotaState(q *pb.QuotaState) ports.QuotaState {
	if q == nil {
		return ports.QuotaState{}
	}
	return ports.QuotaState{Allowed: q.Allowed, WorkspacesUsed: q.WorkspacesUsed, WorkspacesLimit: q.WorkspacesLimit, Reason: q.Reason}
}

func ToProtoBuildRequest(r ports.BuildRequest) *pb.BuildRequest {
	return &pb.BuildRequest{
		WorkspaceId: r.WorkspaceID, SourceRef: r.SourceRef, Provider: r.Provider,
		Options: r.Options, TenantId: r.TenantID,
	}
}
func FromProtoBuildRequest(r *pb.BuildRequest) ports.BuildRequest {
	if r == nil {
		return ports.BuildRequest{}
	}
	return ports.BuildRequest{
		WorkspaceID: r.WorkspaceId, SourceRef: r.SourceRef, Provider: r.Provider,
		Options: r.Options, TenantID: r.TenantId,
	}
}

func ToProtoImageRef(i ports.ImageRef) *pb.ImageRef {
	return &pb.ImageRef{Ref: i.Ref, BuildRef: i.BuildRef}
}
func FromProtoImageRef(i *pb.ImageRef) ports.ImageRef {
	if i == nil {
		return ports.ImageRef{}
	}
	return ports.ImageRef{Ref: i.Ref, BuildRef: i.BuildRef}
}

func ToProtoBuildStatus(b ports.BuildStatus) *pb.BuildStatus {
	return &pb.BuildStatus{Phase: b.Phase, ImageRef: b.ImageRef, Message: b.Message}
}
func FromProtoBuildStatus(b *pb.BuildStatus) ports.BuildStatus {
	if b == nil {
		return ports.BuildStatus{}
	}
	return ports.BuildStatus{Phase: b.Phase, ImageRef: b.ImageRef, Message: b.Message}
}
