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
