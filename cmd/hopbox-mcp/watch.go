package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

// runWatchMode subscribes to a surface's events resource and prints each user
// interaction as it arrives — the AI watching the human drive the canvas, live.
func runWatchMode(connect, host, uri string) {
	if uri == "" {
		fmt.Println("usage: hopbox-mcp watch --connect <addr> hopbox://surface/<name>/events")
		return
	}
	w, r, closeFn := openTransport(connect, host)
	defer closeFn()

	enc := json.NewEncoder(w)
	responses := make(chan map[string]any, 64)
	notifs := make(chan map[string]any, 64)
	go route(r, responses, notifs)
	id := 0
	call := func(method string, params any) map[string]any {
		id++
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		return <-responses
	}
	call("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}})
	call("resources/subscribe", map[string]any{"uri": uri})
	fmt.Printf("👁  watching %s\n    interact in your browser — the AI sees each event here, live:\n\n", uri)

	seen := 0
	show := func() {
		evs := eventsFrom(call("resources/read", map[string]any{"uri": uri}))
		for _, e := range evs[seen:] {
			fmt.Printf("  %s  %-7s %-12s %q\n", time.Unix(e.At, 0).Format("15:04:05"), e.Kind, e.Target, e.Value)
		}
		seen = len(evs)
	}
	show()
	for range notifs { // a push, not a poll
		show()
	}
}

func eventsFrom(m map[string]any) []mcp.SurfaceEvent {
	res, _ := m["result"].(map[string]any)
	contents, _ := res["contents"].([]any)
	if len(contents) == 0 {
		return nil
	}
	c0, _ := contents[0].(map[string]any)
	text, _ := c0["text"].(string)
	var evs []mcp.SurfaceEvent
	_ = json.Unmarshal([]byte(text), &evs)
	return evs
}
