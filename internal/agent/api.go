package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/packages"
	"github.com/hopboxdev/hopbox/internal/service"
	"github.com/hopboxdev/hopbox/internal/snapshot"
)

// maxRPCBodySize caps the request body on /rpc to prevent memory exhaustion
// from oversized payloads. 1 MiB is generous for any legitimate RPC call.
const maxRPCBodySize = 1 << 20 // 1 MiB

// rpcRequest is the JSON-RPC request envelope.
type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is the JSON-RPC response envelope.
type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// registerRoutes wires up the HTTP handlers on the given mux.
func (a *Agent) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/rpc", a.handleRPC)
}

func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"status": "ok", "tunnel": false, "local_ip": ""}
	if a.tunnel != nil {
		s := a.tunnel.Status()
		body["tunnel"] = s.IsUp
		body["local_ip"] = s.LocalIP
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (a *Agent) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRPCBodySize)
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeRPCError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeRPCError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
		return
	}

	switch req.Method {
	case "services.list":
		a.rpcServicesList(w, req)
	case "services.restart":
		a.rpcServicesRestart(w, req)
	case "services.stop":
		a.rpcServicesStop(w, req)
	case "ports.list":
		a.rpcPortsList(w, req)
	case "run.script":
		a.rpcRunScript(w, r, req)
	case "logs.stream":
		a.rpcLogsStream(w, r, req)
	case "packages.install":
		a.rpcPackagesInstall(w, r, req)
	case "snap.create":
		a.rpcSnapCreate(w, r)
	case "snap.restore":
		a.rpcSnapRestore(w, r, req)
	case "snap.list":
		a.rpcSnapList(w, r)
	case "workspace.sync":
		a.rpcWorkspaceSync(w, req)
	default:
		writeRPCError(w, http.StatusNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (a *Agent) rpcServicesList(w http.ResponseWriter, _ rpcRequest) {
	if a.services == nil {
		writeRPCResult(w, []any{})
		return
	}
	writeRPCResult(w, a.services.ListStatus())
}

func (a *Agent) rpcServicesRestart(w http.ResponseWriter, req rpcRequest) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPCError(w, http.StatusBadRequest, "params.name required")
		return
	}
	if a.services == nil {
		writeRPCError(w, http.StatusServiceUnavailable, "service manager not initialised")
		return
	}
	if err := a.services.Restart(params.Name); err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, map[string]string{"status": "restarted"})
}

func (a *Agent) rpcServicesStop(w http.ResponseWriter, req rpcRequest) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPCError(w, http.StatusBadRequest, "params.name required")
		return
	}
	if a.services == nil {
		writeRPCError(w, http.StatusServiceUnavailable, "service manager not initialised")
		return
	}
	if err := a.services.Stop(params.Name); err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, map[string]string{"status": "stopped"})
}

func (a *Agent) rpcPortsList(w http.ResponseWriter, _ rpcRequest) {
	ports, err := service.ListeningPorts()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, ports)
}

func (a *Agent) rpcPackagesInstall(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var params struct {
		Packages []packages.Package `json:"packages"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, http.StatusBadRequest, fmt.Sprintf("decode params: %v", err))
		return
	}
	var installed []string
	var errs []string
	for _, pkg := range params.Packages {
		if err := packages.Install(r.Context(), pkg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", pkg.Name, err))
		} else {
			installed = append(installed, pkg.Name)
		}
	}
	if len(errs) > 0 {
		writeRPCError(w, http.StatusInternalServerError, fmt.Sprintf("install errors: %s", errs))
		return
	}
	writeRPCResult(w, map[string]any{"installed": installed})
}

func (a *Agent) rpcSnapCreate(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	target := a.backupTarget
	paths := make([]string, len(a.backupPaths))
	copy(paths, a.backupPaths)
	a.mu.RUnlock()

	if target == "" {
		writeRPCError(w, http.StatusServiceUnavailable, "no backup target configured")
		return
	}
	if a.services != nil {
		paths = append(paths, a.services.DataPaths()...)
	}
	if len(paths) == 0 {
		writeRPCError(w, http.StatusBadRequest, "no data paths to back up")
		return
	}
	result, err := snapshot.Create(r.Context(), target, paths, nil)
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, result)
}

func (a *Agent) rpcSnapRestore(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	a.mu.RLock()
	target := a.backupTarget
	a.mu.RUnlock()

	if target == "" {
		writeRPCError(w, http.StatusServiceUnavailable, "no backup target configured")
		return
	}
	var params struct {
		ID          string `json:"id"`
		RestorePath string `json:"restore_path"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.ID == "" {
		writeRPCError(w, http.StatusBadRequest, "params.id required")
		return
	}
	if err := snapshot.Restore(r.Context(), target, params.ID, params.RestorePath, nil); err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, map[string]string{"status": "restored", "id": params.ID})
}

func (a *Agent) rpcSnapList(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	target := a.backupTarget
	a.mu.RUnlock()

	if target == "" {
		writeRPCError(w, http.StatusServiceUnavailable, "no backup target configured")
		return
	}
	snaps, err := snapshot.List(r.Context(), target, nil)
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeRPCResult(w, snaps)
}

func (a *Agent) rpcRunScript(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPCError(w, http.StatusBadRequest, "params.name required")
		return
	}
	a.mu.RLock()
	script, ok := a.scripts[params.Name]
	noScripts := len(a.scripts) == 0
	a.mu.RUnlock()

	if noScripts {
		writeRPCError(w, http.StatusServiceUnavailable, "no scripts configured")
		return
	}
	if !ok {
		writeRPCError(w, http.StatusNotFound, fmt.Sprintf("script %q not found", params.Name))
		return
	}
	out, err := exec.CommandContext(r.Context(), "sh", "-c", script).CombinedOutput()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, fmt.Sprintf("script failed: %v\n%s", err, string(out)))
		return
	}
	writeRPCResult(w, map[string]string{"output": string(out)})
}

func (a *Agent) rpcWorkspaceSync(w http.ResponseWriter, req rpcRequest) {
	var params struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.YAML == "" {
		writeRPCError(w, http.StatusBadRequest, "params.yaml required")
		return
	}

	ws, err := manifest.ParseBytes([]byte(params.YAML))
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, fmt.Sprintf("parse manifest: %v", err))
		return
	}

	// Persist manifest to disk (best-effort; agent may not be running as root).
	if mkErr := os.MkdirAll("/etc/hopbox", 0755); mkErr == nil {
		_ = os.WriteFile("/etc/hopbox/hopbox.yaml", []byte(params.YAML), 0644)
	}

	a.Reload(ws)
	writeRPCResult(w, map[string]string{"status": "synced", "name": ws.Name})
}

func (a *Agent) rpcLogsStream(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPCError(w, http.StatusBadRequest, "params.name required")
		return
	}
	out, err := exec.CommandContext(r.Context(), "docker", "logs", "--tail", "100", params.Name).CombinedOutput()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, fmt.Sprintf("docker logs: %v\n%s", err, string(out)))
		return
	}
	writeRPCResult(w, map[string]string{"output": string(out)})
}

func writeRPCResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResponse{Result: result})
}

func writeRPCError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcResponse{Error: msg})
}
