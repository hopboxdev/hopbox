// Package mcp is the hopbox AI-control plane as an MCP server: a subscribable
// fleet resource, box tools (delegate/spawn), and pushed change notifications —
// the protocol from design/ai-control-protocol.md. The protocol is decoupled from
// the box world behind Backend, so the same server runs standalone (ssh backend)
// or inside a daemon (engine backend over box.Engine + the agent hub).
package mcp

import "context"

// Box is one fleet member as the protocol sees it.
type Box struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image,omitempty"`
	State   string `json:"state"` // box phase (running/…) or task state (working/done/failed)
	IP      string `json:"ip,omitempty"`
	Task    string `json:"task,omitempty"`
	Result  string `json:"result,omitempty"`
	Updated int64  `json:"updated"`
}

// Backend is the box world behind the protocol.
type Backend interface {
	// Fleet is the current snapshot (backs hopbox://fleet + fleet.get).
	Fleet(ctx context.Context) []Box
	// Delegate spawns a box and runs task on it, returning the box id immediately;
	// progress + result surface via Fleet + the change signal.
	Delegate(ctx context.Context, task string) (id string, err error)
	// Spawn creates a box with no task.
	Spawn(ctx context.Context, name string) (id string, err error)
	// OnChange registers fn, called whenever the fleet changes; cancel unregisters.
	OnChange(fn func()) (cancel func())
}
