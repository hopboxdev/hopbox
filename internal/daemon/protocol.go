package daemon

import "time"

// Request is sent from a client (hop up, hop down) to the daemon over the Unix socket.
type Request struct {
	Method string `json:"method"` // "status" or "shutdown"
}

// Response is sent from the daemon back to the client.
type Response struct {
	OK    bool          `json:"ok"`
	Error string        `json:"error,omitempty"`
	State *DaemonStatus `json:"state,omitempty"`
}

// DaemonStatus is the live state returned by the "status" method.
type DaemonStatus struct {
	PID         int       `json:"pid"`
	Connected   bool      `json:"connected"`
	LastHealthy time.Time `json:"last_healthy,omitempty"`
	Interface   string    `json:"interface"`
	StartedAt   time.Time `json:"started_at"`
	Bridges     []string  `json:"bridges"`
}
