package plugin_test

import (
	"context"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/plugin"
)

func TestLoadComputeInprocReturnsProvider(t *testing.T) {
	fake := &fakeCompute{}
	got, err := plugin.LoadCompute(plugin.ProviderConfig{Transport: "inproc"}, fake)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := got.Provision(context.Background(), ports.ProvisionRequest{WorkspaceID: "w1"})
	if inst.Ref != "c-w1" {
		t.Fatalf("inproc did not return the given provider: %+v", inst)
	}
}

func TestLoadComputeInprocNilIsError(t *testing.T) {
	if _, err := plugin.LoadCompute(plugin.ProviderConfig{Transport: "inproc"}, nil); err == nil {
		t.Fatal("expected error when inproc provider is nil")
	}
}

func TestLoadComputeRemoteRequiresAddr(t *testing.T) {
	if _, err := plugin.LoadCompute(plugin.ProviderConfig{Transport: "remote"}, nil); err == nil {
		t.Fatal("expected error when remote addr is empty")
	}
}

func TestLoadComputeUnknownTransport(t *testing.T) {
	if _, err := plugin.LoadCompute(plugin.ProviderConfig{Transport: "carrier-pigeon"}, nil); err == nil {
		t.Fatal("expected error for unknown transport")
	}
}

func TestLoadIngressInprocNilIsError(t *testing.T) {
	if _, err := plugin.LoadIngress(plugin.ProviderConfig{Transport: "inproc"}, nil); err == nil {
		t.Fatal("expected error when inproc ingress provider is nil")
	}
}

func TestLoadIngressRemoteRequiresAddr(t *testing.T) {
	if _, err := plugin.LoadIngress(plugin.ProviderConfig{Transport: "remote"}, nil); err == nil {
		t.Fatal("expected error when remote addr is empty")
	}
}

func TestLoadIngressUnknownTransport(t *testing.T) {
	if _, err := plugin.LoadIngress(plugin.ProviderConfig{Transport: "carrier-pigeon"}, nil); err == nil {
		t.Fatal("expected error for unknown transport")
	}
}

func TestLoadIdentityInprocNilIsError(t *testing.T) {
	if _, err := plugin.LoadIdentity(plugin.ProviderConfig{Transport: "inproc"}, nil); err == nil {
		t.Fatal("expected error when inproc identity provider is nil")
	}
}

func TestLoadIdentityRemoteRequiresAddr(t *testing.T) {
	if _, err := plugin.LoadIdentity(plugin.ProviderConfig{Transport: "remote"}, nil); err == nil {
		t.Fatal("expected error when remote addr is empty")
	}
}

func TestLoadIdentityUnknownTransport(t *testing.T) {
	if _, err := plugin.LoadIdentity(plugin.ProviderConfig{Transport: "carrier-pigeon"}, nil); err == nil {
		t.Fatal("expected error for unknown transport")
	}
}
