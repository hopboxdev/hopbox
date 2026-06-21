//go:build docker

package conformance_test

import (
	"os"
	"testing"

	"github.com/hopboxdev/hopbox/internal/core/ports"
	dockerprov "github.com/hopboxdev/hopbox/providers/compute/docker"
	"github.com/hopboxdev/hopbox/test/conformance"
)

func dockerReq(t *testing.T) ports.ProvisionRequest {
	agentBin := os.Getenv("HOPBOX_TEST_AGENT_BIN")
	if agentBin == "" {
		t.Skip("set HOPBOX_TEST_AGENT_BIN to the linux/amd64 hopbox-agent binary")
	}
	return ports.ProvisionRequest{
		WorkspaceID: "conf1",
		ImageRef:    "ubuntu:24.04",
		Agent:       ports.AgentImage{HostBinaryPath: agentBin, TargetPath: "/hopbox/hopbox-agent"},
		Env:         map[string]string{"HOPBOX_CONTROL_ADDR": "host.docker.internal:1", "HOPBOX_AGENT_TOKEN": "x", "HOPBOX_WORKSPACE_ID": "conf1"},
	}
}

func TestDockerInprocConformance(t *testing.T) {
	conformance.RunComputeConformance(t, func(t *testing.T) ports.Compute {
		p, err := dockerprov.New("")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}, dockerReq(t))
}
