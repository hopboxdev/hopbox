// Package api implements the public gRPC WorkspaceService: CRUD over the store
// plus a bidi Shell stream bridged to the workspace agent via the hub.
package api

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
	"github.com/mesadev/mesa/internal/agentproto"
	"github.com/mesadev/mesa/internal/core/store"
	"github.com/mesadev/mesa/internal/core/workspace"
)

// Hub is the subset of agenthub.Hub the API needs (kept small for testing).
type Hub interface {
	Connected(workspaceID string) bool
	OpenShell(ctx context.Context, workspaceID string, hdr agentproto.ShellHeader) (io.ReadWriteCloser, error)
}

type Server struct {
	mesav1.UnimplementedWorkspaceServiceServer
	store  store.Store
	hub    Hub
	tenant string // M1 single tenant
	owner  string // M1 single principal
}

func NewServer(s store.Store, hub Hub, tenant, owner string) *Server {
	return &Server{store: s, hub: hub, tenant: tenant, owner: owner}
}

func toProto(w *workspace.Workspace) *mesav1.Workspace {
	out := &mesav1.Workspace{
		Id: w.ID, TenantId: w.TenantID, Owner: w.Owner, Name: w.Name,
		ImageRef: w.ImageRef, MemMb: w.MemMB, Phase: string(w.Phase),
		AgentConnected: w.AgentConnected, Message: w.Message,
	}
	for _, e := range w.Endpoints {
		out.Endpoints = append(out.Endpoints, &mesav1.Endpoint{Name: e.Name, Url: e.URL, Port: e.Port})
	}
	return out
}

func (s *Server) resolve(ctx context.Context, nameOrID string) (*workspace.Workspace, error) {
	w, err := s.store.GetWorkspace(ctx, s.tenant, nameOrID)
	if err == nil {
		return w, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "resolve %q: %v", nameOrID, err)
	}
	w, err = s.store.GetByName(ctx, s.tenant, nameOrID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "workspace %q not found", nameOrID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve %q: %v", nameOrID, err)
	}
	return w, nil
}

func (s *Server) CreateWorkspace(ctx context.Context, r *mesav1.CreateWorkspaceRequest) (*mesav1.Workspace, error) {
	if r.Name == "" || r.ImageRef == "" {
		return nil, status.Error(codes.InvalidArgument, "name and image_ref are required")
	}
	w := workspace.New(s.tenant, s.owner, r.Name, r.ImageRef)
	w.MemMB = r.MemMb
	for _, ip := range r.Ingress {
		if ip.Name == "" || ip.Port <= 0 {
			return nil, status.Error(codes.InvalidArgument, "ingress entries need a name and port > 0")
		}
		w.Ingress = append(w.Ingress, workspace.IngressPort{Name: ip.Name, Port: ip.Port})
	}
	if err := s.store.CreateWorkspace(ctx, w); err != nil {
		return nil, status.Errorf(codes.Internal, "create: %v", err)
	}
	return toProto(w), nil
}

func (s *Server) GetWorkspace(ctx context.Context, r *mesav1.GetWorkspaceRequest) (*mesav1.Workspace, error) {
	w, err := s.resolve(ctx, r.NameOrId)
	if err != nil {
		return nil, err
	}
	return toProto(w), nil
}

func (s *Server) ListWorkspaces(ctx context.Context, _ *mesav1.ListWorkspacesRequest) (*mesav1.ListWorkspacesResponse, error) {
	ws, err := s.store.ListWorkspaces(ctx, s.tenant)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list: %v", err)
	}
	out := &mesav1.ListWorkspacesResponse{}
	for _, w := range ws {
		out.Workspaces = append(out.Workspaces, toProto(w))
	}
	return out, nil
}

func (s *Server) DeleteWorkspace(ctx context.Context, r *mesav1.DeleteWorkspaceRequest) (*emptypb.Empty, error) {
	w, err := s.resolve(ctx, r.NameOrId)
	if err != nil {
		return nil, err
	}
	// declarative delete: flag Destroying; the reconciler tears down + removes.
	w.Phase = workspace.PhaseDestroying
	if err := s.store.UpdateWorkspace(ctx, w); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "workspace %q not found", r.NameOrId)
		}
		return nil, status.Errorf(codes.Internal, "delete: %v", err)
	}
	return &emptypb.Empty{}, nil
}

// Shell bridges the gRPC bidi stream to a pty stream on the agent.
func (s *Server) Shell(stream mesav1.WorkspaceService_ShellServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "first Shell message must be `open`")
	}
	w, err := s.resolve(ctx, open.NameOrId)
	if err != nil {
		return err
	}
	if !s.hub.Connected(w.ID) {
		return status.Errorf(codes.FailedPrecondition, "workspace %q agent not connected (phase=%s)", w.Name, w.Phase)
	}
	agentStream, err := s.hub.OpenShell(ctx, w.ID, agentproto.ShellHeader{
		Cmd: open.Cmd, Cols: uint16(open.Cols), Rows: uint16(open.Rows),
	})
	if err != nil {
		return status.Errorf(codes.Internal, "open shell: %v", err)
	}
	defer agentStream.Close()

	errc := make(chan error, 2)
	// agent -> client
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, rerr := agentStream.Read(buf)
			if n > 0 {
				if serr := stream.Send(&mesav1.ShellServerMsg{
					Msg: &mesav1.ShellServerMsg_Data{Data: append([]byte(nil), buf[:n]...)},
				}); serr != nil {
					errc <- serr
					return
				}
			}
			if rerr != nil {
				errc <- rerr
				return
			}
		}
	}()
	// client -> agent
	go func() {
		for {
			msg, rerr := stream.Recv()
			if rerr != nil {
				errc <- rerr
				return
			}
			if d := msg.GetData(); d != nil {
				if _, werr := agentStream.Write(d); werr != nil {
					errc <- werr
					return
				}
			}
			// M1: Resize is accepted but not yet forwarded (window-change is a follow-up).
		}
	}()
	err = <-errc
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
