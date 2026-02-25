package agent_test

import (
	"testing"

	"github.com/hopboxdev/hopbox/internal/agent"
	"github.com/hopboxdev/hopbox/internal/manifest"
)

func TestBuildServiceManagerMergesWorkspaceEnv(t *testing.T) {
	ws := &manifest.Workspace{
		Name: "test",
		Env: map[string]string{
			"GLOBAL_A": "from-workspace",
			"SHARED":   "workspace-level",
		},
		Services: map[string]manifest.Service{
			"api": {
				Type:    "native",
				Command: "./server",
				Env: map[string]string{
					"SHARED":    "service-level",
					"SERVICE_B": "only-in-service",
				},
			},
			"worker": {
				Type:    "native",
				Command: "./worker",
				// No service-level env — should inherit workspace env.
			},
		},
	}

	mgr := agent.BuildServiceManager(ws)
	statuses := mgr.ListStatus()

	// Build a lookup by name.
	defByName := map[string]map[string]string{}
	for _, s := range statuses {
		defByName[s.Name] = mgr.Def(s.Name).Env
	}

	// api: SHARED should be "service-level" (service wins), GLOBAL_A inherited.
	apiEnv := defByName["api"]
	if apiEnv["GLOBAL_A"] != "from-workspace" {
		t.Errorf("api GLOBAL_A = %q, want %q", apiEnv["GLOBAL_A"], "from-workspace")
	}
	if apiEnv["SHARED"] != "service-level" {
		t.Errorf("api SHARED = %q, want %q", apiEnv["SHARED"], "service-level")
	}
	if apiEnv["SERVICE_B"] != "only-in-service" {
		t.Errorf("api SERVICE_B = %q, want %q", apiEnv["SERVICE_B"], "only-in-service")
	}

	// worker: should get workspace env only.
	workerEnv := defByName["worker"]
	if workerEnv["GLOBAL_A"] != "from-workspace" {
		t.Errorf("worker GLOBAL_A = %q, want %q", workerEnv["GLOBAL_A"], "from-workspace")
	}
	if workerEnv["SHARED"] != "workspace-level" {
		t.Errorf("worker SHARED = %q, want %q", workerEnv["SHARED"], "workspace-level")
	}
}

func TestBuildServiceManagerNoWorkspaceEnv(t *testing.T) {
	ws := &manifest.Workspace{
		Name: "test",
		Services: map[string]manifest.Service{
			"api": {
				Type:    "native",
				Command: "./server",
				Env:     map[string]string{"KEY": "val"},
			},
		},
	}
	mgr := agent.BuildServiceManager(ws)
	env := mgr.Def("api").Env
	if env["KEY"] != "val" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "val")
	}
}
