//go:build k8s

package kubernetes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

func sampleReq() ports.ProvisionRequest {
	return ports.ProvisionRequest{
		WorkspaceID: "w1",
		ImageRef:    "ubuntu:24.04",
		MemMB:       512,
		Mounts:      []ports.Mount{{Source: "hopbox-home-w1", Target: "/home/dev"}},
		Env:         map[string]string{"HOPBOX_AGENT_TOKEN": "tok", "HOPBOX_WORKSPACE_ID": "w1"},
		Agent:       ports.AgentImage{ImageRef: "ghcr.io/hopboxdev/hopbox-agent:0.2.0", BinaryPath: "/hopbox-agent", TargetPath: "/hopbox/hopbox-agent"},
	}
}

func TestProvisionBuildsAgentInjectingPod(t *testing.T) {
	cli := fake.NewSimpleClientset()
	p := New(cli, "hopbox-workspaces")
	inst, err := p.Provision(context.Background(), sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	if inst.Ref != "hopbox-workspaces/hopbox-w1" {
		t.Fatalf("ref = %q", inst.Ref)
	}
	pod, err := cli.CoreV1().Pods("hopbox-workspaces").Get(context.Background(), "hopbox-w1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pod not created: %v", err)
	}
	// initContainer copies the agent binary to the target
	if len(pod.Spec.InitContainers) != 1 {
		t.Fatalf("want 1 initContainer, got %d", len(pod.Spec.InitContainers))
	}
	ic := pod.Spec.InitContainers[0]
	if ic.Image != "ghcr.io/hopboxdev/hopbox-agent:0.2.0" {
		t.Fatalf("init image = %q", ic.Image)
	}
	if got := ic.Command; len(got) != 3 || got[0] != "cp" || got[1] != "/hopbox-agent" || got[2] != "/hopbox/hopbox-agent" {
		t.Fatalf("init command = %v", got)
	}
	// workspace container runs the agent at TargetPath
	ws := pod.Spec.Containers[0]
	if len(ws.Command) != 1 || ws.Command[0] != "/hopbox/hopbox-agent" {
		t.Fatalf("workspace command = %v", ws.Command)
	}
	if ws.Image != "ubuntu:24.04" {
		t.Fatalf("workspace image = %q", ws.Image)
	}
	// mem limit honored
	if q, ok := ws.Resources.Limits[corev1.ResourceMemory]; !ok || q.Value() != 512*1024*1024 {
		t.Fatalf("mem limit = %v", ws.Resources.Limits)
	}
	// the home mount became a PVC-claim volume (the seam: Mount.Source -> claimName)
	var foundPVC bool
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == "hopbox-home-w1" {
			foundPVC = true
		}
	}
	if !foundPVC {
		t.Fatalf("home PVC volume not found: %+v", pod.Spec.Volumes)
	}
}

func TestProvisionIsIdempotent(t *testing.T) {
	cli := fake.NewSimpleClientset()
	p := New(cli, "hopbox-workspaces")
	if _, err := p.Provision(context.Background(), sampleReq()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Provision(context.Background(), sampleReq()); err != nil {
		t.Fatalf("re-provision (delete-then-create) must succeed: %v", err)
	}
	pods, _ := cli.CoreV1().Pods("hopbox-workspaces").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 1 {
		t.Fatalf("want exactly 1 pod after re-provision, got %d", len(pods.Items))
	}
}

func TestProvisionRejectsHostBinaryOnlyAgent(t *testing.T) {
	p := New(fake.NewSimpleClientset(), "hopbox-workspaces")
	req := sampleReq()
	req.Agent = ports.AgentImage{HostBinaryPath: "/usr/local/bin/hopbox-agent"} // docker-only
	if _, err := p.Provision(context.Background(), req); err == nil {
		t.Fatal("expected error: kubernetes cannot bind-mount a host binary")
	}
}
