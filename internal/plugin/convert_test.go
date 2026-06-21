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
