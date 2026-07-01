package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
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
	cancel := s.be.OnChange(s.notifyAll)
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
			"instructions":    guide,
		})
	case "notifications/initialized":
	case "tools/list":
		s.reply(r.ID, map[string]any{"tools": tools})
	case "tools/call":
		s.toolCall(r)
	case "resources/list":
		s.reply(r.ID, map[string]any{"resources": []map[string]any{
			{"uri": "hopbox://guide", "name": "guide", "mimeType": "text/markdown",
				"description": "how to use hopbox, including the canvas loop for talking to humans"},
			{"uri": "hopbox://fleet", "name": "fleet", "mimeType": "application/json",
				"description": "every box and its live state"},
		}})
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(r.Params, &p)
		if p.URI == "" {
			p.URI = "hopbox://fleet"
		}
		mime, body := "application/json", s.readResource(p.URI)
		if p.URI == "hopbox://guide" {
			mime, body = "text/markdown", guide
		}
		s.reply(r.ID, map[string]any{"contents": []map[string]any{{
			"uri": p.URI, "mimeType": mime, "text": body,
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

// guide is delivered to every client in the initialize response (MCP `instructions`)
// and re-readable as hopbox://guide. It teaches the plane — above all the canvas
// loop, so an AI knows it can talk to humans with interactive UIs, not just text.
const guide = `hopbox is a live control plane for isolated cloud boxes, shared by humans and AIs.
It is EVENT-DRIVEN: subscribe to resources and react to pushed changes — never poll.

RESOURCES (resources/subscribe, then react to notifications/resources/updated):
- hopbox://fleet — every box with its live phase and the agent's self-reported status.
- hopbox://surface/<name>/events — a rendered surface's interaction events.

TOOLS:
- box.delegate {task} — spawn a box, run a task; watch hopbox://fleet for the result.
- fleet.apply {boxes:[{key,image,task}]} — declare a desired set of task-boxes; hopbox
  converges to it, idempotent per key. Prefer this over many box.delegate calls.
- surface.render {name,html} — the CANVAS LOOP, below.
- box.spawn, fleet.get — spawn an empty box / snapshot the fleet.

THE CANVAS LOOP — talk to humans with more than text:
When you need a human's decision, input, approval, or attention, you are NOT limited to
chat. Render an interactive UI and watch them use it, live:
  1. surface.render {name:"approve", html:"<h3>Deploy?</h3><button id=ok>Approve</button>"}
     returns a URL — give it to the human.
  2. resources/subscribe hopbox://surface/approve/events.
  3. Each click/input is PUSHED to you as {kind,target,value}. React: re-render the surface,
     branch your work, or unblock a waiting task. You can even block until a specific
     interaction happens (e.g. an approval), then continue autonomously.
Reach for this for approvals and gates, choices, forms, and dashboards you update live —
anytime showing beats telling. The loop is bidirectional: you render, they act, you
observe, you respond. It is a primary way to collaborate with a human here, not a fallback.`

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
	{"name": "surface.render", "description": "Render an interactive HTML canvas at a URL; the user's clicks/inputs come back via hopbox://surface/<name>/events (the canvas loop).",
		"inputSchema": map[string]any{"type": "object", "required": []string{"name", "html"}, "properties": map[string]any{
			"name": map[string]any{"type": "string"}, "html": map[string]any{"type": "string"}}}},
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
	case "surface.render":
		var a struct {
			Name string `json:"name"`
			HTML string `json:"html"`
		}
		arg(&a)
		url := s.be.RenderSurface(a.Name, a.HTML)
		text(fmt.Sprintf("surface %q at %s — subscribe hopbox://surface/%s/events", a.Name, url, a.Name))
	case "fleet.get":
		text(s.fleetJSON())
	default:
		s.send(resp{JSONRPC: "2.0", ID: r.ID, Error: &rpcErr{-32602, "unknown tool: " + p.Name}})
	}
}

// notifyAll pushes a resources/updated for every subscribed resource on any
// change (fleet box state or a surface interaction); the client re-reads.
func (s *Server) notifyAll() {
	s.mu.Lock()
	uris := make([]string, 0, len(s.subs))
	for u := range s.subs {
		uris = append(uris, u)
	}
	s.mu.Unlock()
	for _, u := range uris {
		s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated",
			"params": map[string]any{"uri": u}})
	}
}

// readResource returns a resource's JSON body: hopbox://fleet or a surface's
// hopbox://surface/<name>/events.
func (s *Server) readResource(uri string) string {
	if rest, ok := strings.CutPrefix(uri, "hopbox://surface/"); ok {
		name := strings.TrimSuffix(rest, "/events")
		if b, _ := json.Marshal(s.be.SurfaceEvents(name)); b != nil {
			return string(b)
		}
		return "[]"
	}
	return s.fleetJSON()
}

func (s *Server) fleetJSON() string {
	b, _ := json.Marshal(s.be.Fleet(context.Background()))
	if b == nil {
		return "[]"
	}
	return string(b)
}
