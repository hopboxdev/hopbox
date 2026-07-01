package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// runDemo wires an in-memory MCP client to the server over two pipes and drives a
// real scenario against box.hopbox.dev: subscribe to the fleet, delegate two
// tasks, then REACT to pushed notifications (re-reading the fleet) until both
// finish. A background reader routes responses vs. notifications onto channels so
// the client never polls — it blocks on the event stream.
func runDemo(s *server) {
	csr, csw := io.Pipe() // client -> server
	scr, scw := io.Pipe() // server -> client
	go s.serve(csr, scw)

	enc := json.NewEncoder(csw)
	responses := make(chan map[string]any, 32)
	notifs := make(chan map[string]any, 32)
	go func() {
		dec := json.NewDecoder(scr)
		for {
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				close(responses)
				return
			}
			if m["method"] != nil {
				notifs <- m
			} else {
				responses <- m
			}
		}
	}()

	start := time.Now()
	ts := func() string { return fmt.Sprintf("[+%4.1fs]", time.Since(start).Seconds()) }
	reqID := 0
	call := func(method string, params any) map[string]any {
		reqID++
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": reqID, "method": method, "params": params})
		return <-responses
	}
	delegate := func(task string) {
		r := call("tools/call", map[string]any{"name": "box.delegate", "arguments": map[string]any{"task": task}})
		fmt.Printf("%s → box.delegate  %-42q  %s\n", ts(), task, toolText(r))
	}

	fmt.Println("hopbox-mcp demo — event-driven, no polling (delegated tasks run on real boxes)")
	fmt.Println("----------------------------------------------------------------------------")
	init := call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}})
	si, _ := init["result"].(map[string]any)["serverInfo"].(map[string]any)
	fmt.Printf("%s ← connected to %v %v\n", ts(), si["name"], si["version"])
	call("resources/subscribe", map[string]any{"uri": "hopbox://fleet"})
	fmt.Printf("%s → subscribed hopbox://fleet\n", ts())

	delegate("echo host=$(hostname); uname -r")
	delegate("python3 -c 'import sys;print(\"python\",sys.version.split()[0])'")

	fmt.Printf("%s ── now blocking on the event stream; the server will PUSH each change ──\n", ts())
	terminal := map[string]bool{}
	for len(terminal) < 2 {
		n := <-notifs // a push, not a poll
		uri, _ := n["params"].(map[string]any)["uri"].(string)
		fmt.Printf("%s ← PUSH  %s  — reacting (resources/read)\n", ts(), uri)
		fleet := fleetFrom(call("resources/read", map[string]any{"uri": "hopbox://fleet"}))
		parts := make([]string, 0, len(fleet))
		for _, b := range fleet {
			parts = append(parts, fmt.Sprintf("%s=%s", b.ID, b.State))
			if b.State == "done" || b.State == "failed" {
				terminal[b.ID] = true
			}
		}
		fmt.Printf("           fleet: %v\n", parts)
	}

	fmt.Printf("%s ✓ all delegated boxes finished — results:\n", ts())
	for _, b := range fleetFrom(call("resources/read", map[string]any{"uri": "hopbox://fleet"})) {
		fmt.Printf("   %s (%s) %s: %s\n", b.ID, b.Name, b.State, oneLine(b.Result))
	}
}

func toolText(m map[string]any) string {
	res, _ := m["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	c0, _ := content[0].(map[string]any)
	t, _ := c0["text"].(string)
	return t
}

func fleetFrom(m map[string]any) []boxState {
	res, _ := m["result"].(map[string]any)
	contents, _ := res["contents"].([]any)
	if len(contents) == 0 {
		return nil
	}
	c0, _ := contents[0].(map[string]any)
	text, _ := c0["text"].(string)
	var bs []boxState
	_ = json.Unmarshal([]byte(text), &bs)
	return bs
}

func oneLine(s string) string {
	out := ""
	for _, r := range s {
		if r == '\n' {
			out += " ⏎ "
		} else {
			out += string(r)
		}
	}
	return out
}
