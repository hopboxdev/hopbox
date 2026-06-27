package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// A small MCP (Model Context Protocol) server over stdio, exposing the box's
// own lifecycle as tools so an AI agent running in the box can manage its
// sandbox (info / keep-alive / auto-suspend / idle). Newline-delimited JSON-RPC
// 2.0, the standard stdio transport — no external SDK.

const mcpProtocol = "2024-11-05"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// tool couples an MCP tool definition with its handler.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	run         func(args map[string]any) (string, error)
}

func schema(props map[string]any) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": props}
}

func tools(b string) []tool {
	return []tool{
		{
			Name: "box_info", Description: "Show this box's metadata (load, idle, suspend settings, resources).",
			InputSchema: schema(nil),
			run:         func(map[string]any) (string, error) { return doGet(b + "/v1/me") },
		},
		{
			Name: "box_keep_alive", Description: "Pin the box alive (prevent auto-suspend) for a Go duration like 30m (default 5m).",
			InputSchema: schema(map[string]any{"duration": map[string]any{"type": "string"}}),
			run: func(a map[string]any) (string, error) {
				return "kept alive", keepAlive(str(a, "duration"))
			},
		},
		{
			Name: "box_auto_suspend", Description: "Enable or disable auto-suspend on idle.",
			InputSchema: schema(map[string]any{"enabled": map[string]any{"type": "boolean"}}),
			run: func(a map[string]any) (string, error) {
				on, _ := a["enabled"].(bool)
				return fmt.Sprintf("auto-suspend=%t", on), autoSuspend(on)
			},
		},
		{
			Name: "box_set_idle", Description: "Set this box's idle timeout (Go duration; empty resets to the daemon default).",
			InputSchema: schema(map[string]any{"timeout": map[string]any{"type": "string"}}),
			run: func(a map[string]any) (string, error) {
				return "idle timeout set", setIdle(str(a, "timeout"))
			},
		},
	}
}

func str(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}

func runMCP(b string) {
	// Run on a terminal? Then a human typed `box-guest mcp` and is staring at a
	// blank prompt — it's a JSON-RPC server, not interactive. Say so.
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "box-guest mcp: MCP server on stdio — waiting for JSON-RPC. Spawn me from an MCP client; Ctrl-C to exit.")
	}
	ts := tools(b)
	byName := map[string]tool{}
	for _, t := range ts {
		byName[t.Name] = t
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := json.NewEncoder(os.Stdout)

	for in.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification (e.g. notifications/initialized): no reply
		}
		_ = out.Encode(dispatch(req, ts, byName))
	}
}

func dispatch(req rpcRequest, ts []tool, byName map[string]tool) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "box-guest", "version": "1"},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": ts}
	case "tools/call":
		resp.Result = callTool(req.Params, byName)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func callTool(params json.RawMessage, byName map[string]tool) map[string]any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(params, &p)
	t, ok := byName[p.Name]
	if !ok {
		return toolResult("unknown tool: "+p.Name, true)
	}
	text, err := t.run(p.Arguments)
	if err != nil {
		return toolResult(p.Name+": "+err.Error(), true)
	}
	return toolResult(text, false)
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}
