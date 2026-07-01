// Command hopbox-mcp is a spine prototype of the hopbox AI-control plane as an MCP
// server (design/ai-control-protocol.md): a subscribable `hopbox://fleet`
// resource, `box.delegate`/`box.spawn`/`fleet.get` tools, and
// notifications/resources/updated pushed on every state change — the event-driven,
// no-poll model. It shells out to `ssh` to run delegated tasks in real boxes.
//
//	hopbox-mcp            # serve MCP over stdio (an AI connects as a client)
//	hopbox-mcp --demo     # self-drive over an in-memory pipe against box.hopbox.dev
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func main() {
	demo := flag.Bool("demo", false, "self-drive a demo over an in-memory MCP pipe")
	host := flag.String("host", "box.hopbox.dev", "boxd front door for delegated boxes")
	flag.Parse()

	s := newServer(*host)
	defer s.cleanup()
	if *demo {
		runDemo(s)
		return
	}
	s.serve(os.Stdin, os.Stdout)
}

// --- fleet state -----------------------------------------------------------

type boxState struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Task    string `json:"task,omitempty"`
	State   string `json:"state"` // working | done | failed
	Result  string `json:"result,omitempty"`
	Updated int64  `json:"updated"`
}

type server struct {
	host    string
	keyFile string

	mu    sync.Mutex
	enc   *json.Encoder // guarded: responses + async notifications share stdout
	fleet map[string]*boxState
	order []string
	subs  map[string]bool
	seq   int
}

func newServer(host string) *server {
	dir, err := os.MkdirTemp("", "hopbox-mcp")
	if err != nil {
		log.Fatal(err)
	}
	key := dir + "/id"
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-q", "-f", key).CombinedOutput(); err != nil {
		log.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	return &server{host: host, keyFile: key, fleet: map[string]*boxState{}, subs: map[string]bool{}}
}

func (s *server) cleanup() { _ = os.RemoveAll(strings.TrimSuffix(s.keyFile, "/id")) }

// --- JSON-RPC / MCP --------------------------------------------------------

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

func (s *server) serve(in io.Reader, out io.Writer) {
	s.enc = json.NewEncoder(out)
	dec := json.NewDecoder(in)
	for {
		var r req
		if err := dec.Decode(&r); err != nil {
			return
		}
		s.handle(r)
	}
}

func (s *server) send(v any) { s.mu.Lock(); _ = s.enc.Encode(v); s.mu.Unlock() }
func (s *server) reply(id json.RawMessage, result any) {
	s.send(resp{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) handle(r req) {
	switch r.Method {
	case "initialize":
		s.reply(r.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{"subscribe": true}},
			"serverInfo":      map[string]any{"name": "hopbox", "version": "0.1-proto"},
		})
	case "notifications/initialized": // no reply
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
			"task": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}}}},
	{"name": "box.spawn", "description": "Spawn a box (no task).",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}},
	{"name": "fleet.get", "description": "Snapshot of every box and its state.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
}

func (s *server) toolCall(r req) {
	var p struct {
		Name      string `json:"name"`
		Arguments struct {
			Task, Name string
		} `json:"arguments"`
	}
	_ = json.Unmarshal(r.Params, &p)
	text := func(t string) {
		s.reply(r.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": t}}, "isError": false})
	}
	switch p.Name {
	case "box.delegate":
		id, name := s.newBox(p.Arguments.Name, p.Arguments.Task)
		go s.delegate(id)
		text(fmt.Sprintf("delegated to box %q (id %s); watch hopbox://fleet", name, id))
	case "box.spawn":
		id, name := s.newBox(p.Arguments.Name, "")
		s.setState(id, "done", "spawned")
		text(fmt.Sprintf("spawned box %q (id %s)", name, id))
	case "fleet.get":
		text(s.fleetJSON())
	default:
		s.send(resp{JSONRPC: "2.0", ID: r.ID, Error: &rpcErr{-32602, "unknown tool: " + p.Name}})
	}
}

// --- boxes -----------------------------------------------------------------

func (s *server) newBox(name, task string) (id, boxName string) {
	s.mu.Lock()
	s.seq++
	id = fmt.Sprintf("b%03d", s.seq)
	if name == "" {
		name = "mcp-" + id
	}
	s.fleet[id] = &boxState{ID: id, Name: name, Task: task, State: "working", Updated: time.Now().Unix()}
	s.order = append(s.order, id)
	s.mu.Unlock()
	go s.notifyFleet()
	return id, name
}

func (s *server) delegate(id string) {
	s.mu.Lock()
	name, task := s.fleet[id].Name, s.fleet[id].Task
	s.mu.Unlock()
	out, err := s.runInBox(name, task)
	if err != nil {
		s.setState(id, "failed", strings.TrimSpace(out+" "+err.Error()))
		return
	}
	s.setState(id, "done", strings.TrimSpace(out))
}

func (s *server) runInBox(name, task string) (string, error) {
	args := []string{"-i", s.keyFile,
		"-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/dev/null", "-o", "ConnectTimeout=90", "-o", "LogLevel=ERROR",
		name + "@" + s.host, task}
	out, err := exec.Command("ssh", args...).CombinedOutput()
	return string(out), err
}

func (s *server) setState(id, st, result string) {
	s.mu.Lock()
	if b := s.fleet[id]; b != nil {
		b.State, b.Result, b.Updated = st, result, time.Now().Unix()
	}
	s.mu.Unlock()
	s.notifyFleet()
}

// notifyFleet pushes a resources/updated notification if anyone subscribed —
// the client reacts and re-reads, never polls.
func (s *server) notifyFleet() {
	s.mu.Lock()
	sub := s.subs["hopbox://fleet"]
	s.mu.Unlock()
	if sub {
		s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated",
			"params": map[string]any{"uri": "hopbox://fleet"}})
	}
}

func (s *server) fleetJSON() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]*boxState, 0, len(s.order))
	for _, id := range s.order {
		list = append(list, s.fleet[id])
	}
	b, _ := json.Marshal(list)
	return string(b)
}
