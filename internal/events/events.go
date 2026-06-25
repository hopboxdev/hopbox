// Package events is the reconcile wake-up seam. State-changers (the agent hub)
// Publish a (workspaceID, tenant) wake-up; the reconciler Subscribes and runs
// ReconcileOne for it. The default InProc bus is a direct call — zero deps,
// fully self-hostable. The NATS bus fans the same wake-ups across nodes so a
// hub on one node can drive a reconciler on another. Either way the reconciler's
// interval sweep remains the backstop, so a dropped/lost wake-up is not fatal.
package events

import (
	"encoding/json"
	"sync"
)

// Handler runs one reconcile wake-up.
type Handler func(workspaceID, tenant string)

// Bus delivers reconcile wake-ups from publishers to subscribed handlers.
type Bus interface {
	Publish(workspaceID, tenant string)
	Subscribe(h Handler) error
	Close() error
}

// InProc is a single-process bus: Publish calls every subscribed handler inline.
type InProc struct {
	mu       sync.RWMutex
	handlers []Handler
}

func NewInProc() *InProc { return &InProc{} }

func (b *InProc) Subscribe(h Handler) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
	return nil
}

func (b *InProc) Publish(workspaceID, tenant string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, h := range b.handlers {
		h(workspaceID, tenant)
	}
}

func (b *InProc) Close() error { return nil }

// Subject is the NATS subject reconcile wake-ups are published on.
const Subject = "hopbox.reconcile"

// Conn is the slice of a NATS connection the bus needs. *nats.Conn is adapted to
// it in nats_conn.go; tests use an in-memory fake. Keeping it an interface means
// the wake-up codec and dispatch are testable without a broker.
type Conn interface {
	Publish(subject string, data []byte) error
	Subscribe(subject string, cb func([]byte)) (unsub func() error, err error)
	Close()
}

type wakeup struct {
	ID     string `json:"id"`
	Tenant string `json:"tenant"`
}

// NATS is a cross-node bus over a Conn.
type NATS struct {
	conn   Conn
	mu     sync.Mutex
	unsubs []func() error
}

func NewNATS(conn Conn) *NATS { return &NATS{conn: conn} }

func (b *NATS) Publish(workspaceID, tenant string) {
	data, err := json.Marshal(wakeup{ID: workspaceID, Tenant: tenant})
	if err != nil {
		return
	}
	_ = b.conn.Publish(Subject, data)
}

func (b *NATS) Subscribe(h Handler) error {
	unsub, err := b.conn.Subscribe(Subject, func(data []byte) {
		var w wakeup
		if err := json.Unmarshal(data, &w); err != nil || w.ID == "" {
			return // ignore malformed wake-ups; the sweep still covers the workspace
		}
		h(w.ID, w.Tenant)
	})
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.unsubs = append(b.unsubs, unsub)
	b.mu.Unlock()
	return nil
}

func (b *NATS) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, u := range b.unsubs {
		_ = u()
	}
	b.conn.Close()
	return nil
}
