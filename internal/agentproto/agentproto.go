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

// ShellHeader is the first frame mesad writes on each new yamux stream; after
// it, the stream is a raw bidirectional pty byte pipe.
type ShellHeader struct {
	Cmd  string `json:"cmd"` // "" => agent default ("/bin/bash")
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
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
func WriteShellHeader(w io.Writer, h ShellHeader) error { return writeFrame(w, h) }
func ReadShellHeader(r io.Reader) (ShellHeader, error)  { var h ShellHeader; return h, readFrame(r, &h) }
