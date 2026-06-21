package agentproto_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/mesadev/mesa/internal/agentproto"
)

func TestHandshakeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := agentproto.Handshake{WorkspaceID: "w1", Token: "tok"}
	if err := agentproto.WriteHandshake(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := agentproto.ReadHandshake(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip %+v != %+v", out, in)
	}
}

func TestShellHeaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := agentproto.ShellHeader{Cmd: "/bin/bash", Cols: 80, Rows: 24}
	if err := agentproto.WriteShellHeader(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := agentproto.ReadShellHeader(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip %+v != %+v", out, in)
	}
}

func TestOpenFrameAndForwardHeaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	of := agentproto.OpenFrame{Kind: agentproto.KindForward}
	if err := agentproto.WriteOpenFrame(&buf, of); err != nil {
		t.Fatal(err)
	}
	if out, err := agentproto.ReadOpenFrame(&buf); err != nil || out != of {
		t.Fatalf("openframe roundtrip: %+v err=%v", out, err)
	}
	fh := agentproto.ForwardHeader{Port: 3000}
	if err := agentproto.WriteForwardHeader(&buf, fh); err != nil {
		t.Fatal(err)
	}
	if out, err := agentproto.ReadForwardHeader(&buf); err != nil || out != fh {
		t.Fatalf("forwardheader roundtrip: %+v err=%v", out, err)
	}
}

func TestExecFramingRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	hdr := agentproto.ExecHeader{Cmd: []string{"echo", "hi"}}
	if err := agentproto.WriteExecHeader(&buf, hdr); err != nil {
		t.Fatal(err)
	}
	if out, err := agentproto.ReadExecHeader(&buf); err != nil || len(out.Cmd) != 2 || out.Cmd[0] != "echo" {
		t.Fatalf("exec header roundtrip: %+v err=%v", out, err)
	}
	// stdout frame, stderr frame, exit frame in order
	if err := agentproto.WriteExecData(&buf, agentproto.ExecStdout, []byte("out")); err != nil {
		t.Fatal(err)
	}
	if err := agentproto.WriteExecData(&buf, agentproto.ExecStderr, []byte("err")); err != nil {
		t.Fatal(err)
	}
	if err := agentproto.WriteExecExit(&buf, 42); err != nil {
		t.Fatal(err)
	}
	typ, data, _, err := agentproto.ReadExecFrame(&buf)
	if err != nil || typ != agentproto.ExecStdout || string(data) != "out" {
		t.Fatalf("stdout frame: typ=%d data=%q err=%v", typ, data, err)
	}
	typ, data, _, err = agentproto.ReadExecFrame(&buf)
	if err != nil || typ != agentproto.ExecStderr || string(data) != "err" {
		t.Fatalf("stderr frame: typ=%d data=%q err=%v", typ, data, err)
	}
	typ, _, code, err := agentproto.ReadExecFrame(&buf)
	if err != nil || typ != agentproto.ExecExit || code != 42 {
		t.Fatalf("exit frame: typ=%d code=%d err=%v", typ, code, err)
	}
}

func TestReadHandshakeRejectsOversizeLength(t *testing.T) {
	// craft a header claiming a body far larger than maxFrame (1<<16)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 1<<20) // 1 MiB > 64 KiB cap
	r := bytes.NewReader(hdr[:])
	if _, err := agentproto.ReadHandshake(r); err == nil {
		t.Fatal("expected error for oversize frame length, got nil")
	}
}

func TestReadHandshakeTruncatedStream(t *testing.T) {
	// valid length header announcing 10 bytes, but only 3 bytes of body follow
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 10)
	buf := append(hdr[:], []byte("abc")...)
	r := bytes.NewReader(buf)
	_, err := agentproto.ReadHandshake(r)
	if err == nil {
		t.Fatal("expected error for truncated body, got nil")
	}
	if err != io.ErrUnexpectedEOF {
		t.Logf("note: got %v (acceptable as long as it errors)", err)
	}
}
