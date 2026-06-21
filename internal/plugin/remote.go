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
// The caller owns conn and must Close it; the returned provider does not.
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
// The caller owns conn and must Close it; the returned provider does not.
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

// remoteIngress implements ports.Ingress over a gRPC connection.
type remoteIngress struct{ cli pb.IngressClient }

var _ ports.Ingress = (*remoteIngress)(nil)

// NewRemoteIngress returns a ports.Ingress backed by a gRPC Ingress service.
// The caller owns conn and must Close it; the returned provider does not.
func NewRemoteIngress(conn *grpc.ClientConn) ports.Ingress {
	return &remoteIngress{cli: pb.NewIngressClient(conn)}
}

func (r *remoteIngress) Expose(ctx context.Context, req ports.ExposeRequest) (ports.Endpoint, error) {
	ep, err := r.cli.Expose(ctx, ToProtoExposeRequest(req))
	if err != nil {
		return ports.Endpoint{}, err
	}
	return FromProtoEndpoint(ep), nil
}
func (r *remoteIngress) Unexpose(ctx context.Context, ref string) error {
	_, err := r.cli.Unexpose(ctx, &pb.EndpointRef{Ref: ref})
	return err
}

// remoteIdentity implements ports.Identity over a gRPC connection.
type remoteIdentity struct{ cli pb.IdentityClient }

var _ ports.Identity = (*remoteIdentity)(nil)

// NewRemoteIdentity returns a ports.Identity backed by a gRPC Identity service.
// The caller owns conn and must Close it; the returned provider does not.
func NewRemoteIdentity(conn *grpc.ClientConn) ports.Identity {
	return &remoteIdentity{cli: pb.NewIdentityClient(conn)}
}

func (r *remoteIdentity) Authenticate(ctx context.Context, c ports.Credential) (ports.Principal, error) {
	p, err := r.cli.Authenticate(ctx, ToProtoCredential(c))
	if err != nil {
		return ports.Principal{}, err
	}
	return FromProtoPrincipal(p), nil
}
func (r *remoteIdentity) Authorize(ctx context.Context, req ports.AccessRequest) (ports.Decision, error) {
	d, err := r.cli.Authorize(ctx, ToProtoAccessRequest(req))
	if err != nil {
		return ports.Decision{}, err
	}
	return FromProtoDecision(d), nil
}
