package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

// Server serves the MCP protocol for one connection against a Backend. Multiple
// connections share one Backend (one plane, many actors).
type Server struct {
	be   Backend
	mu   sync.Mutex // guards enc + subs (responses and async notifications share the writer)
	enc  *json.Encoder
	subs map[string]bool
}

func NewServer(be Backend) *Server { return &Server{be: be, subs: map[string]bool{}} }

// Serve runs the protocol over in/out until in closes.
func (s *Server) Serve(in io.Reader, out io.Writer) {
	s.enc = json.NewEncoder(out)
	cancel := s.be.OnChange(func() { s.notifyFleet() })
	defer cancel()
	dec := json.NewDecoder(in)
	for {
		var r req
		if err := dec.Decode(&r); err != nil {
			return
		}
		s.handle(r)
	}
}

// Listen accepts connections on ln, serving each as an MCP session sharing be.
func Listen(ctx context.Context, ln net.Listener, be Backend) error {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go func() { defer c.Close(); NewServer(be).Serve(c, c) }()
	}
}

type req struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}
type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}
type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) send(v any) { s.mu.Lock(); _ = s.enc.Encode(v); s.mu.Unlock() }
func (s *Server) reply(id json.RawMessage, result any) {
	s.send(resp{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) handle(r req) {
	switch r.Method {
	case "initialize":
		s.reply(r.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{"subscribe": true}},
			"serverInfo":      map[string]any{"name": "hopbox", "version": "0.1"},
		})
	case "notifications/initialized":
	case "tools/list":
		s.reply(r.ID, map[string]any{"tools": tools})
	case "tools/call":
		s.toolCall(r)
	case "resources/list":
		s.reply(r.ID, map[string]any{"resources": []map[string]any{{
			"uri": "hopbox://fleet", "name": "fleet", "mimeType": "application/json",
			"description": "every box and its live state",
		}}})
	case "resources/read":
		s.reply(r.ID, map[string]any{"contents": []map[string]any{{
			"uri": "hopbox://fleet", "mimeType": "application/json", "text": s.fleetJSON(),
		}}})
	case "resources/subscribe":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(r.Params, &p)
		s.mu.Lock()
		s.subs[p.URI] = true
		s.mu.Unlock()
		s.reply(r.ID, map[string]any{})
	default:
		if len(r.ID) > 0 {
			s.send(resp{JSONRPC: "2.0", ID: r.ID, Error: &rpcErr{-32601, "method not found: " + r.Method}})
		}
	}
}

var tools = []map[string]any{
	{"name": "box.delegate", "description": "Spawn a box and run a task on it. Returns immediately with an id; watch hopbox://fleet for completion.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"task"}, "properties": map[string]any{
			"task": map[string]any{"type": "string"}}}},
	{"name": "box.spawn", "description": "Spawn a box (no task).",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}},
	{"name": "fleet.apply", "description": "Declare a desired set of task-boxes and converge to it (idempotent per key). Spawns each key whose box is absent; watch hopbox://fleet.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"boxes"}, "properties": map[string]any{
			"boxes": map[string]any{"type": "array", "items": map[string]any{"type": "object",
				"required": []string{"key"}, "properties": map[string]any{
					"key":   map[string]any{"type": "string"},
					"image": map[string]any{"type": "string"},
					"task":  map[string]any{"type": "string"}}}}}}},
	{"name": "fleet.get", "description": "Snapshot of every box and its state.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
}

func (s *Server) toolCall(r req) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(r.Params, &p)
	ctx := context.Background()
	arg := func(v any) { _ = json.Unmarshal(p.Arguments, v) }
	text := func(t string) {
		s.reply(r.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": t}}, "isError": false})
	}
	fail := func(e error) {
		s.reply(r.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": e.Error()}}, "isError": true})
	}
	switch p.Name {
	case "box.delegate":
		var a struct {
			Task string `json:"task"`
		}
		arg(&a)
		id, err := s.be.Delegate(ctx, a.Task)
		if err != nil {
			fail(err)
			return
		}
		text(fmt.Sprintf("delegated (box %s); watch hopbox://fleet", id))
	case "box.spawn":
		var a struct {
			Name string `json:"name"`
		}
		arg(&a)
		id, err := s.be.Spawn(ctx, a.Name)
		if err != nil {
			fail(err)
			return
		}
		text(fmt.Sprintf("spawned box %s", id))
	case "fleet.apply":
		var a struct {
			Boxes []SpecBox `json:"boxes"`
		}
		arg(&a)
		created, err := s.be.Apply(ctx, a.Boxes)
		if err != nil {
			fail(err)
			return
		}
		text(fmt.Sprintf("applied %d box(es): %d created, %d already present; watch hopbox://fleet",
			len(a.Boxes), len(created), len(a.Boxes)-len(created)))
	case "fleet.get":
		text(s.fleetJSON())
	default:
		s.send(resp{JSONRPC: "2.0", ID: r.ID, Error: &rpcErr{-32602, "unknown tool: " + p.Name}})
	}
}

func (s *Server) notifyFleet() {
	s.mu.Lock()
	sub := s.subs["hopbox://fleet"]
	s.mu.Unlock()
	if sub {
		s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated",
			"params": map[string]any{"uri": "hopbox://fleet"}})
	}
}

func (s *Server) fleetJSON() string {
	b, _ := json.Marshal(s.be.Fleet(context.Background()))
	if b == nil {
		return "[]"
	}
	return string(b)
}
