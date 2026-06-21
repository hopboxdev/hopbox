package conformance_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/hopboxdev/hopbox/gen/hopbox/provider/v1"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/plugin"
	"github.com/hopboxdev/hopbox/internal/plugin/server"
	"github.com/hopboxdev/hopbox/providers/identity/static"
	"github.com/hopboxdev/hopbox/test/conformance"
)

func staticKeys() map[string]ports.Principal {
	return map[string]ports.Principal{
		"secret-key-1": {ID: "alice", TenantID: "default", DisplayName: "Alice", Roles: []string{"owner"}},
	}
}

var (
	validCred   = ports.Credential{Scheme: "api-key", Value: "secret-key-1"}
	invalidCred = ports.Credential{Scheme: "api-key", Value: "nope"}
)

func TestStaticIdentityInproc(t *testing.T) {
	conformance.RunIdentityConformance(t, func(t *testing.T) ports.Identity {
		return static.New(staticKeys())
	}, validCred, invalidCred)
}

func TestStaticIdentityOverRemote(t *testing.T) {
	conformance.RunIdentityConformance(t, func(t *testing.T) ports.Identity {
		lis := bufconn.Listen(1 << 20)
		gs := grpc.NewServer()
		pb.RegisterIdentityServer(gs, server.NewIdentity(static.New(staticKeys())))
		go func() { _ = gs.Serve(lis) }()
		t.Cleanup(gs.Stop)
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return plugin.NewRemoteIdentity(conn)
	}, validCred, invalidCred)
}
