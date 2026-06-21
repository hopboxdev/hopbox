package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
)

// The gateway tunnel lets a standalone mesa-gw reach workspaces hosted by a
// central mesad. mesa-gw dials the tunnel and sends a TunnelHeader naming the
// request Host; mesad resolves it (server-side) and bridges the raw byte pipe to
// the workspace's agent forward stream. This keeps mesa-gw stateless — it owns
// no route table and no agent sessions; it just forwards Hosts.
//
// Wire format: a 4-byte big-endian length prefix + JSON, one TunnelHeader from
// the client then one TunnelResponse from the server; after a Status=="ok"
// response the conn is a raw bidirectional pipe carrying the proxied HTTP.

type TunnelHeader struct {
	Host string `json:"host"`
}

type TunnelResponse struct {
	Status string `json:"status"` // "ok" | "no_route" | "error"
	Reason string `json:"reason,omitempty"`
}

const maxTunnelFrame = 1 << 16

func writeFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxTunnelFrame {
		return fmt.Errorf("gateway tunnel: frame too large (%d)", len(b))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func readFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxTunnelFrame {
		return fmt.Errorf("gateway tunnel: frame too large (%d)", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// TunnelServer serves the in-process Connector to remote mesa-gw processes over
// a raw TCP listener.
type TunnelServer struct{ connector Connector }

func NewTunnelServer(c Connector) *TunnelServer { return &TunnelServer{connector: c} }

// Serve accepts tunnel dials until ctx is cancelled or the listener closes.
func (s *TunnelServer) Serve(ctx context.Context, ln net.Listener) error {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.handle(ctx, conn)
	}
}

func (s *TunnelServer) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	var hdr TunnelHeader
	if err := readFrame(conn, &hdr); err != nil {
		return
	}
	upstream, err := s.connector.Connect(ctx, hdr.Host)
	if err == ErrNoRoute {
		_ = writeFrame(conn, TunnelResponse{Status: "no_route"})
		return
	}
	if err != nil {
		_ = writeFrame(conn, TunnelResponse{Status: "error", Reason: err.Error()})
		return
	}
	defer upstream.Close()
	if err := writeFrame(conn, TunnelResponse{Status: "ok"}); err != nil {
		return
	}
	// bridge the raw byte pipe in both directions until either side closes.
	errc := make(chan error, 2)
	go func() { _, e := io.Copy(upstream, conn); errc <- e }()
	go func() { _, e := io.Copy(conn, upstream); errc <- e }()
	if err := <-errc; err != nil {
		log.Printf("gateway tunnel: bridge %q: %v", hdr.Host, err)
	}
}

// RemoteConnector implements Connector by dialing a mesad gateway tunnel. It is
// the heart of the standalone mesa-gw: stateless, it forwards the Host and lets
// mesad resolve + bridge.
type RemoteConnector struct {
	addr string
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

var _ Connector = (*RemoteConnector)(nil)

func NewRemoteConnector(tunnelAddr string) *RemoteConnector {
	return &RemoteConnector{addr: tunnelAddr}
}

func (c *RemoteConnector) Connect(ctx context.Context, host string) (net.Conn, error) {
	dial := c.dial
	if dial == nil {
		dial = func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		}
	}
	conn, err := dial(ctx, c.addr)
	if err != nil {
		return nil, fmt.Errorf("gateway tunnel: dial %q: %w", c.addr, err)
	}
	if err := writeFrame(conn, TunnelHeader{Host: host}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var resp TunnelResponse
	if err := readFrame(conn, &resp); err != nil {
		_ = conn.Close()
		return nil, err
	}
	switch resp.Status {
	case "ok":
		return conn, nil
	case "no_route":
		_ = conn.Close()
		return nil, ErrNoRoute
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("gateway tunnel: %s", resp.Reason)
	}
}
