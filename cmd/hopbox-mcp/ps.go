package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

// runPSMode renders the fleet at a glance: subscribe to hopbox://fleet and redraw
// on every pushed change (name · image · phase · agent state · status line) — the
// herdr-style "see agent state at a glance" view, driven by the event stream.
func runPSMode(connect, host string, once bool) {
	w, r, closeFn := openTransport(connect, host)
	defer closeFn()
	runPS(w, r, once)
}

func runPS(w io.Writer, r io.Reader, once bool) {
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
	call("resources/subscribe", map[string]any{"uri": "hopbox://fleet"})

	render := func() {
		if !once {
			fmt.Print("\033[2J\033[H") // clear + home for the live glance
		}
		printFleetTable(fleetFrom(call("resources/read", map[string]any{"uri": "hopbox://fleet"})))
	}
	render()
	if once {
		return
	}
	for range notifs { // a push, not a poll
		render()
	}
}

func printFleetTable(fleet []mcp.Box) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "BOX\tNAME\tIMAGE\tPHASE\tAGENT\tSTATUS")
	for _, b := range fleet {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			short(b.ID), dash(b.Name), dash(b.Image), dash(b.State), dash(b.AgentState), dash(b.AgentStatus))
	}
	_ = tw.Flush()
	fmt.Printf("\n%d box(es)  ·  %s\n", len(fleet), time.Now().Format("15:04:05"))
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
