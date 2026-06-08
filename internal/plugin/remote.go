package plugin

import (
	"context"

	"google.golang.org/grpc"

	pb "github.com/mesadev/mesa/gen/mesa/provider/v1"
	"github.com/mesadev/mesa/internal/core/ports"
)

// remoteCompute implements ports.Compute over a gRPC connection.
type remoteCompute struct{ cli pb.ComputeClient }

var _ ports.Compute = (*remoteCompute)(nil)

// NewRemoteCompute returns a ports.Compute backed by a gRPC Compute service.
func NewRemoteCompute(conn *grpc.ClientConn) ports.Compute {
	return &remoteCompute{cli: pb.NewComputeClient(conn)}
}

func (r *remoteCompute) Provision(ctx context.Context, req ports.ProvisionRequest) (ports.Instance, error) {
	inst, err := r.cli.Provision(ctx, ToProtoProvisionRequest(req))
	if err != nil {
		return ports.Instance{}, err
	}
	return FromProtoInstance(inst), nil
}
func (r *remoteCompute) Status(ctx context.Context, ref string) (ports.Instance, error) {
	inst, err := r.cli.Status(ctx, &pb.InstanceRef{Ref: ref})
	if err != nil {
		return ports.Instance{}, err
	}
	return FromProtoInstance(inst), nil
}
func (r *remoteCompute) Stop(ctx context.Context, ref string) error {
	_, err := r.cli.Stop(ctx, &pb.InstanceRef{Ref: ref})
	return err
}
func (r *remoteCompute) Destroy(ctx context.Context, ref string) error {
	_, err := r.cli.Destroy(ctx, &pb.InstanceRef{Ref: ref})
	return err
}

// remoteStorage implements ports.Storage over a gRPC connection.
type remoteStorage struct{ cli pb.StorageClient }

var _ ports.Storage = (*remoteStorage)(nil)

// NewRemoteStorage returns a ports.Storage backed by a gRPC Storage service.
func NewRemoteStorage(conn *grpc.ClientConn) ports.Storage {
	return &remoteStorage{cli: pb.NewStorageClient(conn)}
}

func (r *remoteStorage) EnsureHome(ctx context.Context, req ports.HomeRequest) (ports.Mount, error) {
	m, err := r.cli.EnsureHome(ctx, ToProtoHomeRequest(req))
	if err != nil {
		return ports.Mount{}, err
	}
	return FromProtoMount(m), nil
}
func (r *remoteStorage) Delete(ctx context.Context, ref string) error {
	_, err := r.cli.Delete(ctx, &pb.HomeRef{Source: ref})
	return err
}
