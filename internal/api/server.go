// Package api implements the public gRPC WorkspaceService: CRUD over the store
// plus a bidi Shell stream bridged to the workspace agent via the hub.
package api

import (
	"context"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
	"github.com/hopboxdev/hopbox/internal/sshca"
)

// certTTL is how long an issued user certificate is valid.
const certTTL = 12 * time.Hour

// Hub is the subset of agenthub.Hub the API needs (kept small for testing).
type Hub interface {
	Connected(workspaceID string) bool
	OpenShell(ctx context.Context, workspaceID string, hdr agentproto.ShellHeader) (io.ReadWriteCloser, error)
	OpenExec(workspaceID string, cmd []string) (io.ReadWriteCloser, error)
	OpenSSH(workspaceID string) (io.ReadWriteCloser, error)
}

type Server struct {
	hopboxv1.UnimplementedWorkspaceServiceServer
	store  store.Store
	hub    Hub
	tenant string     // M1 single tenant
	owner  string     // M1 single principal
	ca     ssh.Signer // SSH user CA for `hopbox login` (nil = SSH-cert login disabled)
}

func NewServer(s store.Store, hub Hub, tenant, owner string, ca ssh.Signer) *Server {
	return &Server{store: s, hub: hub, tenant: tenant, owner: owner, ca: ca}
}

// IssueSSHCert signs the caller's SSH public key into a short-lived user
// certificate for their principal. In M1 the principal is the single configured
// owner; once API callers are authenticated it becomes the caller's identity.
func (s *Server) IssueSSHCert(_ context.Context, req *hopboxv1.IssueSSHCertRequest) (*hopboxv1.IssueSSHCertResponse, error) {
	if s.ca == nil {
		return nil, status.Error(codes.FailedPrecondition, "ssh certificate login is not enabled on this server")
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(req.PublicKey))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parse public key: %v", err)
	}
	cert, err := sshca.SignUserCert(s.ca, pub, s.owner+"@hopbox", []string{s.owner}, certTTL)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "sign cert: %v", err)
	}
	return &hopboxv1.IssueSSHCertResponse{
		Certificate:     string(sshca.MarshalCert(cert)),
		Principal:       s.owner,
		ValidBeforeUnix: int64(cert.ValidBefore),
	}, nil
}

func toProto(w *workspace.Workspace) *hopboxv1.Workspace {
	out := &hopboxv1.Workspace{
		Id: w.ID, TenantId: w.TenantID, Owner: w.Owner, Name: w.Name,
		ImageRef: w.ImageRef, MemMb: w.MemMB, Phase: string(w.Phase),
		AgentConnected: w.AgentConnected, Message: w.Message,
	}
	for _, e := range w.Endpoints {
		out.Endpoints = append(out.Endpoints, &hopboxv1.Endpoint{Name: e.Name, Url: e.URL, Port: e.Port})
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

func (s *Server) CreateWorkspace(ctx context.Context, r *hopboxv1.CreateWorkspaceRequest) (*hopboxv1.Workspace, error) {
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

func (s *Server) GetWorkspace(ctx context.Context, r *hopboxv1.GetWorkspaceRequest) (*hopboxv1.Workspace, error) {
	w, err := s.resolve(ctx, r.NameOrId)
	if err != nil {
		return nil, err
	}
	return toProto(w), nil
}

func (s *Server) ListWorkspaces(ctx context.Context, _ *hopboxv1.ListWorkspacesRequest) (*hopboxv1.ListWorkspacesResponse, error) {
	ws, err := s.store.ListWorkspaces(ctx, s.tenant)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list: %v", err)
	}
	out := &hopboxv1.ListWorkspacesResponse{}
	for _, w := range ws {
		out.Workspaces = append(out.Workspaces, toProto(w))
	}
	return out, nil
}

func (s *Server) DeleteWorkspace(ctx context.Context, r *hopboxv1.DeleteWorkspaceRequest) (*emptypb.Empty, error) {
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

// Exec runs a non-interactive command on the agent, forwarding client stdin and
// streaming framed stdout/stderr back, finishing with the exit code.
func (s *Server) Exec(stream hopboxv1.WorkspaceService_ExecServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "first Exec message must be `open`")
	}
	if len(open.Cmd) == 0 {
		return status.Error(codes.InvalidArgument, "cmd is required")
	}
	w, err := s.resolve(ctx, open.NameOrId)
	if err != nil {
		return err
	}
	if !s.hub.Connected(w.ID) {
		return status.Errorf(codes.FailedPrecondition, "workspace %q agent not connected (phase=%s)", w.Name, w.Phase)
	}
	agentStream, err := s.hub.OpenExec(w.ID, open.Cmd)
	if err != nil {
		return status.Errorf(codes.Internal, "open exec: %v", err)
	}
	defer agentStream.Close()

	// stdin pump: client -> agent (framed), until the client half-closes.
	go func() {
		for {
			msg, rerr := stream.Recv()
			if rerr != nil {
				_ = agentproto.WriteExecStdinClose(agentStream)
				return
			}
			if d := msg.GetStdin(); d != nil {
				if werr := agentproto.WriteExecData(agentStream, agentproto.ExecStdin, d); werr != nil {
					return
				}
			}
		}
	}()

	for {
		typ, data, code, rerr := agentproto.ReadExecFrame(agentStream)
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return status.Errorf(codes.Internal, "exec read: %v", rerr)
		}
		switch typ {
		case agentproto.ExecStdout:
			if err := stream.Send(&hopboxv1.ExecServerMsg{Msg: &hopboxv1.ExecServerMsg_Stdout{Stdout: data}}); err != nil {
				return err
			}
		case agentproto.ExecStderr:
			if err := stream.Send(&hopboxv1.ExecServerMsg{Msg: &hopboxv1.ExecServerMsg_Stderr{Stderr: data}}); err != nil {
				return err
			}
		case agentproto.ExecExit:
			return stream.Send(&hopboxv1.ExecServerMsg{Msg: &hopboxv1.ExecServerMsg_ExitCode{ExitCode: code}})
		}
	}
}

// Shell bridges the gRPC bidi stream to a pty stream on the agent.
func (s *Server) Shell(stream hopboxv1.WorkspaceService_ShellServer) error {
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
				if serr := stream.Send(&hopboxv1.ShellServerMsg{
					Msg: &hopboxv1.ShellServerMsg_Data{Data: append([]byte(nil), buf[:n]...)},
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

// SSH bridges a raw SSH transport between the client (`hopbox proxy`, used as an
// OpenSSH ProxyCommand) and the workspace agent's embedded SSH server. The
// control plane never inspects the bytes; auth is the user's SSH key, verified
// inside the workspace.
func (s *Server) SSH(stream hopboxv1.WorkspaceService_SSHServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "first SSH message must be `open`")
	}
	w, err := s.resolve(stream.Context(), open.NameOrId)
	if err != nil {
		return err
	}
	if !s.hub.Connected(w.ID) {
		return status.Errorf(codes.FailedPrecondition, "workspace %q agent not connected (phase=%s)", w.Name, w.Phase)
	}
	agentStream, err := s.hub.OpenSSH(w.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "open ssh: %v", err)
	}
	defer agentStream.Close()

	errc := make(chan error, 2)
	// agent -> client
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, rerr := agentStream.Read(buf)
			if n > 0 {
				if serr := stream.Send(&hopboxv1.SSHServerMsg{Data: append([]byte(nil), buf[:n]...)}); serr != nil {
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
		}
	}()
	err = <-errc
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
