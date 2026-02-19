package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hopboxdev/hopbox/internal/service"
)

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

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func writeRPCResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResponse{Result: result})
}

func writeRPCError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcResponse{Error: msg})
}

// parseHostPort splits "host:port" for use in URLs.
func parseHostPort(addr string) (host, port string) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, ""
	}
	return addr[:i], addr[i+1:]
}
