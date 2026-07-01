package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

// runDemoMode drives an MCP demo: subscribe to the fleet, delegate two tasks, then
// REACT to pushed notifications until both finish — no polling. Against a daemon
// socket (connect != "") it exercises the real engine backend; otherwise it runs a
// standalone ssh backend over an in-memory pipe.
func runDemoMode(connect, host string) {
	if connect != "" {
		fmt.Printf("(driving the daemon MCP plane at %s)\n", connect)
	}
	w, r, closeFn := openTransport(connect, host)
	defer closeFn()
	runDemoClient(w, r)
}

func runDemoClient(w io.Writer, r io.Reader) {
	enc := json.NewEncoder(w)
	responses := make(chan map[string]any, 64)
	notifs := make(chan map[string]any, 64)
	go route(r, responses, notifs)

	start := time.Now()
	ts := func() string { return fmt.Sprintf("[+%4.1fs]", time.Since(start).Seconds()) }
	reqID := 0
	call := func(method string, params any) map[string]any {
		reqID++
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": reqID, "method": method, "params": params})
		return <-responses
	}
	delegate := func(task string) string {
		txt := toolText(call("tools/call", map[string]any{"name": "box.delegate", "arguments": map[string]any{"task": task}}))
		fmt.Printf("%s → box.delegate  %-46q  %s\n", ts(), task, txt)
		return idFrom(txt)
	}

	fmt.Println("hopbox-mcp demo — event-driven, no polling (tasks run on real boxes)")
	fmt.Println("-------------------------------------------------------------------")
	init := call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}})
	si, _ := init["result"].(map[string]any)["serverInfo"].(map[string]any)
	fmt.Printf("%s ← connected to %v %v\n", ts(), si["name"], si["version"])
	call("resources/subscribe", map[string]any{"uri": "hopbox://fleet"})
	fmt.Printf("%s → subscribed hopbox://fleet\n", ts())

	mine := map[string]bool{}
	mine[delegate("echo host=$(hostname); uname -r")] = true
	mine[delegate("python3 -c 'import sys;print(\"python\",sys.version.split()[0])'")] = true
	delete(mine, "")

	fmt.Printf("%s ── blocking on the event stream; the server PUSHes each change ──\n", ts())
	for {
		<-notifs // a push, not a poll
		fleet := fleetFrom(call("resources/read", map[string]any{"uri": "hopbox://fleet"}))
		done, parts := 0, []string{}
		for _, b := range fleet {
			if mine[b.ID] {
				parts = append(parts, fmt.Sprintf("%s=%s", b.ID, b.State))
				if b.State == "done" || b.State == "failed" {
					done++
				}
			}
		}
		fmt.Printf("%s ← PUSH  my boxes: %v\n", ts(), parts)
		if done == len(mine) {
			break
		}
	}

	fmt.Printf("%s ✓ my delegated boxes finished — results:\n", ts())
	for _, b := range fleetFrom(call("resources/read", map[string]any{"uri": "hopbox://fleet"})) {
		if mine[b.ID] {
			fmt.Printf("   %s (%s) %s: %s\n", b.ID, b.Name, b.State, oneLine(b.Result))
		}
	}
}

func idFrom(txt string) string {
	if i := strings.Index(txt, "box "); i >= 0 {
		f := strings.Fields(txt[i+4:])
		if len(f) > 0 {
			return strings.TrimRight(f[0], ");")
		}
	}
	return ""
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

func fleetFrom(m map[string]any) []mcp.Box {
	res, _ := m["result"].(map[string]any)
	contents, _ := res["contents"].([]any)
	if len(contents) == 0 {
		return nil
	}
	c0, _ := contents[0].(map[string]any)
	text, _ := c0["text"].(string)
	var bs []mcp.Box
	_ = json.Unmarshal([]byte(text), &bs)
	return bs
}

func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", " ⏎ ")
}
