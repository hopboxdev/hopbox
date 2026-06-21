//go:build docker

package docker_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
)

func TestProvisionRunsAgentAndDestroy(t *testing.T) {
	agentBin := os.Getenv("HOPBOX_TEST_AGENT_BIN")
	if agentBin == "" {
		t.Skip("set HOPBOX_TEST_AGENT_BIN to the linux/amd64 hopbox-agent binary")
	}
	ctx := context.Background()
	p, err := dockerprov.New("host.docker.internal:7777")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	inst, err := p.Provision(ctx, ports.ProvisionRequest{
		WorkspaceID: "itest1",
		ImageRef:    "ubuntu:24.04",
		Agent:       ports.AgentImage{HostBinaryPath: agentBin, TargetPath: "/hopbox/hopbox-agent"},
		Env: map[string]string{
			"HOPBOX_AGENT_TOKEN":  "tok",
			"HOPBOX_CONTROL_ADDR": "host.docker.internal:7777",
			"HOPBOX_WORKSPACE_ID": "itest1",
		},
		Mounts: []ports.Mount{{Source: t.TempDir(), Target: "/home/dev"}},
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.Ref) })

	time.Sleep(2 * time.Second)
	st, err := p.Status(ctx, inst.Ref)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Phase != ports.InstanceRunning {
		t.Fatalf("phase=%s want running", st.Phase)
	}
	if err := p.Destroy(ctx, inst.Ref); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if st, _ := p.Status(ctx, inst.Ref); st.Phase != ports.InstanceGone {
		t.Fatalf("phase=%s want gone after destroy", st.Phase)
	}
}
