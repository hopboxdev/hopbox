// Package agentproto defines the small length-prefixed JSON frames exchanged
// between hopboxd and hopbox-agent: a one-time handshake on the raw conn, and a
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
	KindExec    = "exec"
	KindSSH     = "ssh"  // raw SSH transport; the agent serves an SSH server on it
	KindSFTP    = "sftp" // the agent serves an SFTP server directly on the stream
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

// ExecHeader follows an OpenFrame{Kind:exec}; the agent runs Cmd (argv, no pty)
// and streams its output back as exec frames (no stdin in v1).
type ExecHeader struct {
	Cmd []string `json:"cmd"`
}

// Exec output frame types (agent -> controller), a binary framing on the exec
// stream: [type:1][len:4][payload]. stdout/stderr payloads are raw bytes; the
// exit payload is a 4-byte big-endian int32 process exit code (sent last).
const (
	ExecStdout     byte = 1
	ExecStderr     byte = 2
	ExecExit       byte = 3
	ExecStdin      byte = 4 // controller -> agent: stdin bytes
	ExecStdinClose byte = 5 // controller -> agent: no more stdin (EOF cmd.Stdin)
)

// WriteExecData writes a stdout/stderr frame. Callers must chunk payloads to
// <= maxFrame; the agent's exec writer does this.
func WriteExecData(w io.Writer, typ byte, data []byte) error {
	if len(data) > maxFrame {
		return fmt.Errorf("agentproto: exec data frame too large (%d)", len(data))
	}
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// WriteExecStdinClose signals end-of-stdin to the agent (EOFs cmd.Stdin).
func WriteExecStdinClose(w io.Writer) error {
	var hdr [5]byte
	hdr[0] = ExecStdinClose
	binary.BigEndian.PutUint32(hdr[1:], 0)
	_, err := w.Write(hdr[:])
	return err
}

// WriteExecExit writes the terminal exit frame.
func WriteExecExit(w io.Writer, code int32) error {
	var buf [9]byte
	buf[0] = ExecExit
	binary.BigEndian.PutUint32(buf[1:5], 4)
	binary.BigEndian.PutUint32(buf[5:9], uint32(code))
	_, err := w.Write(buf[:])
	return err
}

// ReadExecFrame reads one exec frame. For ExecExit, code is set and data is nil;
// otherwise data holds the stdout/stderr bytes.
func ReadExecFrame(r io.Reader) (typ byte, data []byte, code int32, err error) {
	var h [5]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return
	}
	typ = h[0]
	n := binary.BigEndian.Uint32(h[1:])
	if n > maxFrame {
		err = fmt.Errorf("agentproto: exec frame too large (%d)", n)
		return
	}
	payload := make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return
	}
	if typ == ExecExit {
		if len(payload) != 4 {
			err = fmt.Errorf("agentproto: bad exit frame length %d", len(payload))
			return
		}
		code = int32(binary.BigEndian.Uint32(payload))
		return
	}
	data = payload
	return
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

func WriteExecHeader(w io.Writer, h ExecHeader) error { return writeFrame(w, h) }
func ReadExecHeader(r io.Reader) (ExecHeader, error) {
	var h ExecHeader
	return h, readFrame(r, &h)
}
