//go:build docker

package conformance_test

import (
	"os"
	"testing"

	"github.com/mesadev/mesa/internal/core/ports"
	dockerprov "github.com/mesadev/mesa/providers/compute/docker"
	"github.com/mesadev/mesa/test/conformance"
)

func dockerReq(t *testing.T) ports.ProvisionRequest {
	agentBin := os.Getenv("MESA_TEST_AGENT_BIN")
	if agentBin == "" {
		t.Skip("set MESA_TEST_AGENT_BIN to the linux/amd64 mesa-agent binary")
	}
	return ports.ProvisionRequest{
		WorkspaceID: "conf1",
		ImageRef:    "ubuntu:24.04",
		Agent:       ports.AgentImage{HostBinaryPath: agentBin, TargetPath: "/mesa/mesa-agent"},
		Env:         map[string]string{"MESA_CONTROL_ADDR": "host.docker.internal:1", "MESA_AGENT_TOKEN": "x", "MESA_WORKSPACE_ID": "conf1"},
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
