package events_test

import (
	"testing"

	"github.com/hopboxdev/hopbox/internal/events"
)

func TestInProcDeliversToHandler(t *testing.T) {
	bus := events.NewInProc()
	var gotID, gotTenant string
	if err := bus.Subscribe(func(id, tenant string) { gotID, gotTenant = id, tenant }); err != nil {
		t.Fatal(err)
	}
	bus.Publish("w1", "default")
	if gotID != "w1" || gotTenant != "default" {
		t.Fatalf("handler got %q/%q want w1/default", gotID, gotTenant)
	}
}

// fakeConn is an in-memory NATS stand-in: Publish synchronously fans out to the
// callbacks subscribed on the same subject. No broker required.
type fakeConn struct {
	subs map[string][]func([]byte)
}

func newFakeConn() *fakeConn { return &fakeConn{subs: map[string][]func([]byte){}} }

func (c *fakeConn) Publish(subj string, data []byte) error {
	for _, cb := range c.subs[subj] {
		cb(data)
	}
	return nil
}

func (c *fakeConn) Subscribe(subj string, cb func([]byte)) (func() error, error) {
	c.subs[subj] = append(c.subs[subj], cb)
	return func() error { return nil }, nil
}

func (c *fakeConn) Close() {}

func TestNATSRoundTripViaConn(t *testing.T) {
	conn := newFakeConn()
	bus := events.NewNATS(conn)
	defer bus.Close()

	var gotID, gotTenant string
	if err := bus.Subscribe(func(id, tenant string) { gotID, gotTenant = id, tenant }); err != nil {
		t.Fatal(err)
	}
	bus.Publish("w42", "tenantA")
	if gotID != "w42" || gotTenant != "tenantA" {
		t.Fatalf("handler got %q/%q want w42/tenantA", gotID, gotTenant)
	}
}

func TestNATSIgnoresMalformedMessage(t *testing.T) {
	conn := newFakeConn()
	bus := events.NewNATS(conn)
	defer bus.Close()

	called := false
	if err := bus.Subscribe(func(string, string) { called = true }); err != nil {
		t.Fatal(err)
	}
	// raw garbage delivered directly on the subject must not panic or dispatch.
	_ = conn.Publish(events.Subject, []byte("not-json"))
	if called {
		t.Fatal("handler must not fire on a malformed message")
	}
}
