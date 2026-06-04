package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
)

// shellStream is the subset of the gRPC bidi client stream pump() needs.
type shellStream interface {
	Send(*mesav1.ShellClientMsg) error
	Recv() (*mesav1.ShellServerMsg, error)
}

// pump bridges local stdin/stdout to the shell stream. Returns the exit code.
func pump(stream shellStream, stdin io.Reader, stdout io.Writer) int {
	exit := make(chan int, 1)
	// stdin -> server
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				if serr := stream.Send(&mesav1.ShellClientMsg{
					Msg: &mesav1.ShellClientMsg_Data{Data: append([]byte(nil), buf[:n]...)},
				}); serr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// server -> stdout
	go func() {
		code := 0
		for {
			msg, err := stream.Recv()
			if err != nil {
				exit <- code
				return
			}
			if d := msg.GetData(); d != nil {
				_, _ = stdout.Write(d)
			}
			if ec := msg.GetExitCode(); ec != 0 || msg.GetData() == nil {
				code = int(ec)
			}
		}
	}()
	return <-exit
}

func newShellCmd(dial func() (mesav1.WorkspaceServiceClient, func(), error)) *cobra.Command {
	return &cobra.Command{
		Use:   "shell <name|id>",
		Short: "Open an interactive shell in a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()

			stream, err := client.Shell(context.Background())
			if err != nil {
				return err
			}
			cols, rows := 80, 24
			if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
				cols, rows = w, h
			}
			if err := stream.Send(&mesav1.ShellClientMsg{Msg: &mesav1.ShellClientMsg_Open{
				Open: &mesav1.OpenShell{NameOrId: args[0], Cols: uint32(cols), Rows: uint32(rows)},
			}}); err != nil {
				return err
			}

			// raw mode so keystrokes pass straight through
			var restore func()
			if term.IsTerminal(int(os.Stdin.Fd())) {
				old, err := term.MakeRaw(int(os.Stdin.Fd()))
				if err == nil {
					restore = func() { _ = term.Restore(int(os.Stdin.Fd()), old) }
				}
			}
			code := pump(stream, os.Stdin, os.Stdout)
			if restore != nil {
				restore()
			}
			if code != 0 {
				return errors.New("shell exited non-zero")
			}
			return nil
		},
	}
}
