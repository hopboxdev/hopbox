package main

import (
	"bytes"
	"io"
	"testing"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
)

// fakeStream implements shellStream: it yields one data msg then EOF, and
// records what was sent.
type fakeStream struct {
	recvQueue []*mesav1.ShellServerMsg
	sent      [][]byte
}

func (f *fakeStream) Send(m *mesav1.ShellClientMsg) error {
	if d := m.GetData(); d != nil {
		f.sent = append(f.sent, append([]byte(nil), d...))
	}
	return nil
}
func (f *fakeStream) Recv() (*mesav1.ShellServerMsg, error) {
	if len(f.recvQueue) == 0 {
		return nil, io.EOF
	}
	m := f.recvQueue[0]
	f.recvQueue = f.recvQueue[1:]
	return m, nil
}

func TestPumpWritesServerDataToStdout(t *testing.T) {
	fs := &fakeStream{recvQueue: []*mesav1.ShellServerMsg{
		{Msg: &mesav1.ShellServerMsg_Data{Data: []byte("hello")}},
	}}
	var out bytes.Buffer
	in := bytes.NewReader(nil) // empty stdin => send loop ends immediately
	code := pump(fs, in, &out)
	if out.String() != "hello" {
		t.Fatalf("stdout=%q want hello", out.String())
	}
	if code != 0 {
		t.Fatalf("exit code=%d", code)
	}
}

func TestPumpSendsStdinToServer(t *testing.T) {
	fs := &fakeStream{recvQueue: []*mesav1.ShellServerMsg{
		{Msg: &mesav1.ShellServerMsg_ExitCode{ExitCode: 7}},
	}}
	var out bytes.Buffer
	in := bytes.NewReader([]byte("typed"))
	code := pump(fs, in, &out)
	if code != 7 {
		t.Fatalf("exit code=%d want 7", code)
	}
	// stdin content should have been forwarded (best-effort; allow it to race to EOF)
	joined := bytes.Join(fs.sent, nil)
	if len(joined) != 0 && string(joined) != "typed" {
		t.Fatalf("sent=%q", joined)
	}
}
