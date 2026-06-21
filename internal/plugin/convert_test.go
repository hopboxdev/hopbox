package plugin

import (
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
)

func TestProvisionRequestRoundTrip(t *testing.T) {
	in := ports.ProvisionRequest{
		WorkspaceID: "w1",
		ImageRef:    "ubuntu:24.04",
		MemMB:       512,
		Mounts:      []ports.Mount{{Source: "/data/w1", Target: "/home/dev", ReadOnly: false}},
		Env:         map[string]string{"MESA_AGENT_TOKEN": "tok", "MESA_WORKSPACE_ID": "w1"},
		Agent:       ports.AgentImage{ImageRef: "img:1", BinaryPath: "/a", TargetPath: "/mesa/mesa-agent", HostBinaryPath: ""},
	}
	got := FromProtoProvisionRequest(ToProtoProvisionRequest(in))
	if got.WorkspaceID != in.WorkspaceID || got.ImageRef != in.ImageRef || got.MemMB != in.MemMB {
		t.Fatalf("scalar mismatch: %+v", got)
	}
	if len(got.Mounts) != 1 || got.Mounts[0] != in.Mounts[0] {
		t.Fatalf("mounts mismatch: %+v", got.Mounts)
	}
	if got.Env["MESA_AGENT_TOKEN"] != "tok" || got.Agent != in.Agent {
		t.Fatalf("env/agent mismatch: %+v", got)
	}
}

func TestInstancePhaseRoundTrip(t *testing.T) {
	for _, ph := range []ports.InstancePhase{ports.InstanceRunning, ports.InstanceStopped, ports.InstanceGone, ports.InstanceFailed} {
		in := ports.Instance{Ref: "c1", Phase: ph}
		got := FromProtoInstance(ToProtoInstance(in))
		if got != in {
			t.Fatalf("instance round-trip: in=%+v got=%+v", in, got)
		}
	}
}

func TestExposeRequestAndEndpointRoundTrip(t *testing.T) {
	er := ports.ExposeRequest{WorkspaceID: "w1", Name: "app", Port: 3000, Scheme: "subdomain", TenantID: "default"}
	if got := FromProtoExposeRequest(ToProtoExposeRequest(er)); got != er {
		t.Fatalf("exposerequest round-trip: %+v", got)
	}
	ep := ports.Endpoint{Ref: "app-w1.gw.host", URL: "https://app-w1.gw.host", Name: "app", Port: 3000}
	if got := FromProtoEndpoint(ToProtoEndpoint(ep)); got != ep {
		t.Fatalf("endpoint round-trip: %+v", got)
	}
}

func TestIdentityRoundTrip(t *testing.T) {
	cred := ports.Credential{Scheme: "api-key", Value: "secret-1"}
	if got := FromProtoCredential(ToProtoCredential(cred)); got != cred {
		t.Fatalf("credential round-trip: %+v", got)
	}
	pr := ports.Principal{ID: "alice", TenantID: "default", DisplayName: "Alice", Roles: []string{"owner", "tenant-admin"}}
	got := FromProtoPrincipal(ToProtoPrincipal(pr))
	if got.ID != pr.ID || got.TenantID != pr.TenantID || got.DisplayName != pr.DisplayName || len(got.Roles) != 2 || got.Roles[0] != "owner" {
		t.Fatalf("principal round-trip: %+v", got)
	}
	ar := ports.AccessRequest{Principal: pr, Action: "workspace.create", Resource: "default"}
	if g := FromProtoAccessRequest(ToProtoAccessRequest(ar)); g.Action != ar.Action || g.Resource != ar.Resource || g.Principal.ID != pr.ID {
		t.Fatalf("accessrequest round-trip: %+v", g)
	}
	d := ports.Decision{Allow: true, Reason: "ok"}
	if g := FromProtoDecision(ToProtoDecision(d)); g != d {
		t.Fatalf("decision round-trip: %+v", g)
	}
}

func TestHomeRequestAndMountRoundTrip(t *testing.T) {
	hr := ports.HomeRequest{WorkspaceID: "w1", TenantID: "default", Owner: "alice"}
	if got := FromProtoHomeRequest(ToProtoHomeRequest(hr)); got != hr {
		t.Fatalf("homerequest round-trip: %+v", got)
	}
	m := ports.Mount{Source: "pvc-1", Target: "/home/dev", ReadOnly: true}
	if got := FromProtoMount(ToProtoMount(m)); got != m {
		t.Fatalf("mount round-trip: %+v", got)
	}
}
