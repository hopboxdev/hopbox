package agentproto_test

import (
	"bytes"
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
