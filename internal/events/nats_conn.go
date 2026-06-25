package events

import "github.com/nats-io/nats.go"

// natsConn adapts *nats.Conn to the Conn interface. It is the only part of the
// NATS bus that touches the client library; the codec and dispatch live in
// events.go and are broker-free testable.
type natsConn struct{ nc *nats.Conn }

// Connect dials a NATS server and returns a cross-node reconcile bus.
func Connect(url string) (*NATS, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}
	return NewNATS(&natsConn{nc: nc}), nil
}

func (c *natsConn) Publish(subject string, data []byte) error {
	return c.nc.Publish(subject, data)
}

func (c *natsConn) Subscribe(subject string, cb func([]byte)) (func() error, error) {
	sub, err := c.nc.Subscribe(subject, func(m *nats.Msg) { cb(m.Data) })
	if err != nil {
		return nil, err
	}
	return sub.Unsubscribe, nil
}

func (c *natsConn) Close() { c.nc.Close() }
