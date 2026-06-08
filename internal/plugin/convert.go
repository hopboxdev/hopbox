// Package plugin is the provider-plane adapter + loader. It is the ONLY place
// where generated protobuf types and the hand-written internal/core/ports
// types meet. The core never imports the generated wire package.
package plugin

import (
	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
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
