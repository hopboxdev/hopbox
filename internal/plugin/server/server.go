// Package server hosts a ports provider as a gRPC service (the server side of
// the remote transport). It translates pb requests into ports calls.
package server

import (
	"context"

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
