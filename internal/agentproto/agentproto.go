// Package agentproto defines the small length-prefixed JSON frames exchanged
// between mesad and mesa-agent: a one-time handshake on the raw conn, and a
// per-stream shell header sent at the start of each yamux stream.
package agentproto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Handshake is the first frame the agent writes on the raw TCP conn, before
// yamux starts. M1 auth = the one-time bootstrap token. (mTLS is a follow-up.)
type Handshake struct {
	WorkspaceID string `json:"workspace_id"`
	Token       string `json:"token"`
}

// Stream kinds carried by OpenFrame.
const (
	KindShell   = "shell"
	KindForward = "forward"
)

// OpenFrame is the first frame on every yamux stream; Kind selects the handler.
// A kind-specific header follows (ShellHeader for shell, ForwardHeader for
// forward), then the stream is a raw bidirectional byte pipe.
type OpenFrame struct {
	Kind string `json:"kind"`
}

// ShellHeader follows an OpenFrame{Kind:shell}; after it the stream is a raw
// bidirectional pty byte pipe.
type ShellHeader struct {
	Cmd  string `json:"cmd"` // "" => agent default ("/bin/bash")
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// ForwardHeader follows an OpenFrame{Kind:forward}; the agent dials
// 127.0.0.1:Port inside the workspace and pipes the stream to it.
type ForwardHeader struct {
	Port uint32 `json:"port"`
}

const maxFrame = 1 << 16

func writeFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxFrame {
		return fmt.Errorf("agentproto: frame too large (%d)", len(b))
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
	if n > maxFrame {
		return fmt.Errorf("agentproto: frame too large (%d)", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func WriteHandshake(w io.Writer, h Handshake) error     { return writeFrame(w, h) }
func ReadHandshake(r io.Reader) (Handshake, error)      { var h Handshake; return h, readFrame(r, &h) }
func WriteOpenFrame(w io.Writer, f OpenFrame) error     { return writeFrame(w, f) }
func ReadOpenFrame(r io.Reader) (OpenFrame, error)      { var f OpenFrame; return f, readFrame(r, &f) }
func WriteShellHeader(w io.Writer, h ShellHeader) error { return writeFrame(w, h) }
func ReadShellHeader(r io.Reader) (ShellHeader, error)  { var h ShellHeader; return h, readFrame(r, &h) }

func WriteForwardHeader(w io.Writer, h ForwardHeader) error { return writeFrame(w, h) }
func ReadForwardHeader(r io.Reader) (ForwardHeader, error) {
	var h ForwardHeader
	return h, readFrame(r, &h)
}
