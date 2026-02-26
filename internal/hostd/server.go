//go:build linux

package hostd

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"github.com/hopboxdev/silo"

	pb "github.com/hopboxdev/hopbox/gen/hostd/v1"
)

// Server implements the HostService gRPC server.
type Server struct {
	pb.UnimplementedHostServiceServer
	rt          *silo.Runtime
	provisioner *Provisioner
	ports       *PortAllocator
	hostIP      string
	defaults    WorkspaceDefaults
}

// WorkspaceDefaults holds default values for workspace creation.
type WorkspaceDefaults struct {
	Image    string
	VCPUs    int
	MemoryMB int
	DiskGB   int
}

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	Runtime       *silo.Runtime
	Provisioner   *Provisioner
	PortAllocator *PortAllocator
	HostIP        string
	Defaults      WorkspaceDefaults
}

// NewServer creates a new gRPC server.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		rt:          cfg.Runtime,
		provisioner: cfg.Provisioner,
		ports:       cfg.PortAllocator,
		hostIP:      cfg.HostIP,
		defaults:    cfg.Defaults,
	}
}

func (s *Server) CreateWorkspace(ctx context.Context, req *pb.CreateWorkspaceRequest) (*pb.CreateWorkspaceResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	image := req.Image
	if image == "" {
		image = s.defaults.Image
	}
	vcpus := int(req.Vcpus)
	if vcpus == 0 {
		vcpus = s.defaults.VCPUs
	}
	memMB := int(req.MemoryMb)
	if memMB == 0 {
		memMB = s.defaults.MemoryMB
	}
	diskGB := int(req.DiskGb)
	if diskGB == 0 {
		diskGB = s.defaults.DiskGB
	}

	log.Printf("[hostd] creating workspace %q", req.Name)

	// Allocate a port first (so we can clean up if VM creation fails).
	hostPort, err := s.ports.Allocate(req.Name)
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}

	// Use a background context for VM lifecycle operations so the VM
	// survives after the gRPC request context is done.
	vmCtx := context.Background()

	// Create and start VM.
	vm, err := s.rt.Create(vmCtx, silo.VMConfig{
		Name:      req.Name,
		Image:     image,
		VCPUs:     vcpus,
		MemoryMB:  memMB,
		DiskGB:    diskGB,
		Lifecycle: silo.Persistent,
	})
	if err != nil {
		_ = s.ports.Release(req.Name)
		return nil, fmt.Errorf("create VM: %w", err)
	}

	if err := vm.Start(vmCtx); err != nil {
		_ = vm.Destroy(vmCtx)
		_ = s.ports.Release(req.Name)
		return nil, fmt.Errorf("start VM: %w", err)
	}

	// Provision: inject agent, exchange keys, start agent, port forward.
	result, err := s.provisioner.Provision(vmCtx, vm, hostPort)
	if err != nil {
		_ = vm.Destroy(vmCtx)
		_ = s.ports.Release(req.Name)
		return nil, fmt.Errorf("provision: %w", err)
	}

	log.Printf("[hostd] workspace %q ready on port %d", req.Name, hostPort)

	return &pb.CreateWorkspaceResponse{
		Workspace: workspaceInfo(vm, hostPort),
		ClientConfig: &pb.ClientConfig{
			Name:          req.Name,
			Endpoint:      fmt.Sprintf("%s:%d", s.hostIP, hostPort),
			PrivateKey:    result.ClientPrivateKey,
			PeerPublicKey: result.ServerPublicKey,
			TunnelIp:      "10.10.0.1/24",
			AgentIp:       "10.10.0.2",
		},
	}, nil
}

func (s *Server) DestroyWorkspace(_ context.Context, req *pb.DestroyWorkspaceRequest) (*pb.DestroyWorkspaceResponse, error) {
	vmCtx := context.Background()
	vm, err := s.rt.Get(vmCtx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	// Clean up iptables rules.
	if port, err := s.ports.Get(req.Name); err == nil {
		s.provisioner.Deprovision(vm.IP(), port)
	}

	if err := vm.Destroy(vmCtx); err != nil {
		return nil, fmt.Errorf("destroy: %w", err)
	}

	_ = s.ports.Release(req.Name)

	log.Printf("[hostd] workspace %q destroyed", req.Name)
	return &pb.DestroyWorkspaceResponse{}, nil
}

func (s *Server) SuspendWorkspace(_ context.Context, req *pb.SuspendWorkspaceRequest) (*pb.SuspendWorkspaceResponse, error) {
	vmCtx := context.Background()
	vm, err := s.rt.Get(vmCtx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	// Clean up iptables before suspend (TAP will be released).
	if port, err := s.ports.Get(req.Name); err == nil {
		s.provisioner.Deprovision(vm.IP(), port)
	}

	if err := vm.Suspend(vmCtx); err != nil {
		return nil, fmt.Errorf("suspend: %w", err)
	}

	log.Printf("[hostd] workspace %q suspended", req.Name)
	return &pb.SuspendWorkspaceResponse{}, nil
}

func (s *Server) ResumeWorkspace(_ context.Context, req *pb.ResumeWorkspaceRequest) (*pb.ResumeWorkspaceResponse, error) {
	vmCtx := context.Background()
	vm, err := s.rt.Get(vmCtx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	if err := vm.Resume(vmCtx); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}

	// Re-provision port forwarding (VM has new TAP IP after resume).
	hostPort, err := s.ports.Get(req.Name)
	if err != nil {
		return nil, fmt.Errorf("get port: %w", err)
	}
	if err := s.provisioner.setupPortForward(vm.IP(), hostPort); err != nil {
		return nil, fmt.Errorf("port forward: %w", err)
	}

	log.Printf("[hostd] workspace %q resumed on port %d", req.Name, hostPort)

	return &pb.ResumeWorkspaceResponse{
		Workspace: workspaceInfo(vm, hostPort),
	}, nil
}

func (s *Server) GetWorkspace(ctx context.Context, req *pb.GetWorkspaceRequest) (*pb.GetWorkspaceResponse, error) {
	vm, err := s.rt.Get(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get VM: %w", err)
	}

	port, _ := s.ports.Get(req.Name)

	return &pb.GetWorkspaceResponse{
		Workspace: workspaceInfo(vm, port),
	}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, _ *pb.ListWorkspacesRequest) (*pb.ListWorkspacesResponse, error) {
	vms, err := s.rt.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list VMs: %w", err)
	}

	infos := make([]*pb.WorkspaceInfo, len(vms))
	for i, vm := range vms {
		port, _ := s.ports.Get(vm.Name)
		infos[i] = workspaceInfo(vm, port)
	}

	return &pb.ListWorkspacesResponse{
		Workspaces: infos,
	}, nil
}

func (s *Server) HostStatus(_ context.Context, _ *pb.HostStatusRequest) (*pb.HostStatusResponse, error) {
	totalCPUs := runtime.NumCPU()

	// TODO: more accurate capacity tracking when needed
	return &pb.HostStatusResponse{
		TotalVcpus:     int32(totalCPUs),
		AvailableVcpus: int32(totalCPUs), // placeholder
	}, nil
}

func workspaceInfo(vm *silo.VM, hostPort int) *pb.WorkspaceInfo {
	return &pb.WorkspaceInfo{
		Name:     vm.Name,
		State:    string(vm.State()),
		Vcpus:    int32(vm.Config.VCPUs),
		MemoryMb: int32(vm.Config.MemoryMB),
		DiskGb:   int32(vm.Config.DiskGB),
		HostPort: int32(hostPort),
		VmIp:     vm.IP(),
	}
}
