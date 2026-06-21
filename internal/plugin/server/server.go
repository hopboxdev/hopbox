// Package server hosts a ports provider as a gRPC service (the server side of
// the remote transport). It translates pb requests into ports calls.
package server

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc"

	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/plugin"
)

// ComputeServer adapts a ports.Compute to the gRPC Compute service.
type ComputeServer struct {
	pb.UnimplementedComputeServer
	impl ports.Compute
}

func NewCompute(impl ports.Compute) *ComputeServer { return &ComputeServer{impl: impl} }

func (s *ComputeServer) Provision(ctx context.Context, r *pb.ProvisionRequest) (*pb.Instance, error) {
	inst, err := s.impl.Provision(ctx, plugin.FromProtoProvisionRequest(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoInstance(inst), nil
}
func (s *ComputeServer) Status(ctx context.Context, r *pb.InstanceRef) (*pb.Instance, error) {
	inst, err := s.impl.Status(ctx, r.Ref)
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoInstance(inst), nil
}
func (s *ComputeServer) Stop(ctx context.Context, r *pb.InstanceRef) (*pb.Empty, error) {
	return &pb.Empty{}, s.impl.Stop(ctx, r.Ref)
}
func (s *ComputeServer) Destroy(ctx context.Context, r *pb.InstanceRef) (*pb.Empty, error) {
	return &pb.Empty{}, s.impl.Destroy(ctx, r.Ref)
}

// StorageServer adapts a ports.Storage to the gRPC Storage service.
type StorageServer struct {
	pb.UnimplementedStorageServer
	impl ports.Storage
}

func NewStorage(impl ports.Storage) *StorageServer { return &StorageServer{impl: impl} }

func (s *StorageServer) EnsureHome(ctx context.Context, r *pb.HomeRequest) (*pb.Mount, error) {
	m, err := s.impl.EnsureHome(ctx, plugin.FromProtoHomeRequest(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoMount(m), nil
}
func (s *StorageServer) Delete(ctx context.Context, r *pb.HomeRef) (*pb.Empty, error) {
	return &pb.Empty{}, s.impl.Delete(ctx, r.Source)
}

// IngressServer adapts a ports.Ingress to the gRPC Ingress service.
type IngressServer struct {
	pb.UnimplementedIngressServer
	impl ports.Ingress
}

func NewIngress(impl ports.Ingress) *IngressServer { return &IngressServer{impl: impl} }

func (s *IngressServer) Expose(ctx context.Context, r *pb.ExposeRequest) (*pb.Endpoint, error) {
	ep, err := s.impl.Expose(ctx, plugin.FromProtoExposeRequest(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoEndpoint(ep), nil
}
func (s *IngressServer) Unexpose(ctx context.Context, r *pb.EndpointRef) (*pb.Empty, error) {
	return &pb.Empty{}, s.impl.Unexpose(ctx, r.Ref)
}

// IdentityServer adapts a ports.Identity to the gRPC Identity service.
type IdentityServer struct {
	pb.UnimplementedIdentityServer
	impl ports.Identity
}

func NewIdentity(impl ports.Identity) *IdentityServer { return &IdentityServer{impl: impl} }

func (s *IdentityServer) Authenticate(ctx context.Context, r *pb.Credential) (*pb.Principal, error) {
	p, err := s.impl.Authenticate(ctx, plugin.FromProtoCredential(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoPrincipal(p), nil
}
func (s *IdentityServer) Authorize(ctx context.Context, r *pb.AccessRequest) (*pb.Decision, error) {
	d, err := s.impl.Authorize(ctx, plugin.FromProtoAccessRequest(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoDecision(d), nil
}

// MeteringServer adapts a ports.Metering to the gRPC Metering service.
type MeteringServer struct {
	pb.UnimplementedMeteringServer
	impl ports.Metering
}

func NewMetering(impl ports.Metering) *MeteringServer { return &MeteringServer{impl: impl} }

// Emit drains the client stream, dispatching each event to the in-process
// provider, then closes with Empty.
func (s *MeteringServer) Emit(stream grpc.ClientStreamingServer[pb.UsageEvent, pb.Empty]) error {
	ctx := stream.Context()
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.Empty{})
		}
		if err != nil {
			return err
		}
		if err := s.impl.Emit(ctx, plugin.FromProtoUsageEvent(ev)); err != nil {
			return err
		}
	}
}
func (s *MeteringServer) Quota(ctx context.Context, r *pb.PrincipalRef) (*pb.QuotaState, error) {
	q, err := s.impl.Quota(ctx, plugin.FromProtoPrincipalRef(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoQuotaState(q), nil
}

// BuildServer adapts a ports.Build to the gRPC Build service.
type BuildServer struct {
	pb.UnimplementedBuildServer
	impl ports.Build
}

func NewBuild(impl ports.Build) *BuildServer { return &BuildServer{impl: impl} }

func (s *BuildServer) Resolve(ctx context.Context, r *pb.BuildRequest) (*pb.ImageRef, error) {
	img, err := s.impl.Resolve(ctx, plugin.FromProtoBuildRequest(r))
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoImageRef(img), nil
}
func (s *BuildServer) Status(ctx context.Context, r *pb.BuildRef) (*pb.BuildStatus, error) {
	st, err := s.impl.Status(ctx, r.BuildRef)
	if err != nil {
		return nil, err
	}
	return plugin.ToProtoBuildStatus(st), nil
}
