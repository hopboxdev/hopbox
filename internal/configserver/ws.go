package configserver

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	// No origin check needed — server is loopback-only, reached via SSH tunnel.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HeartbeatManager tracks WebSocket clients and calls shutdown when the last
// one disconnects, after a grace period. The grace period prevents shutdown
// on page reload (which briefly has zero clients).
type HeartbeatManager struct {
	mu       sync.Mutex
	clients  int
	timer    *time.Timer
	shutdown func()
	grace    time.Duration
}

// NewHeartbeatManager creates a HeartbeatManager. shutdown is called once
// when the grace period expires with zero connected clients.
func NewHeartbeatManager(shutdown func(), grace time.Duration) *HeartbeatManager {
	return &HeartbeatManager{shutdown: shutdown, grace: grace}
}

// Handler returns an http.HandlerFunc that upgrades to WebSocket and tracks connections.
func (h *HeartbeatManager) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		h.mu.Lock()
		h.clients++
		if h.timer != nil {
			h.timer.Stop()
			h.timer = nil
		}
		h.mu.Unlock()

		defer func() {
			conn.Close()
			h.mu.Lock()
			h.clients--
			if h.clients == 0 {
				h.timer = time.AfterFunc(h.grace, h.shutdown)
			}
			h.mu.Unlock()
		}()

		// Block until connection closes (read loop drains any pings from client).
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
}
