package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
)

// shellStream is the subset of the gRPC bidi client stream pump() needs.
type shellStream interface {
	Send(*hopboxv1.ShellClientMsg) error
	Recv() (*hopboxv1.ShellServerMsg, error)
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
				if serr := stream.Send(&hopboxv1.ShellClientMsg{
					Msg: &hopboxv1.ShellClientMsg_Data{Data: append([]byte(nil), buf[:n]...)},
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
			if _, ok := msg.Msg.(*hopboxv1.ShellServerMsg_ExitCode); ok {
				code = int(msg.GetExitCode())
			}
		}
	}()
	return <-exit
}

func newShellCmd(dial func() (hopboxv1.WorkspaceServiceClient, func(), error)) *cobra.Command {
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
			if err := stream.Send(&hopboxv1.ShellClientMsg{Msg: &hopboxv1.ShellClientMsg_Open{
				Open: &hopboxv1.OpenShell{NameOrId: args[0], Cols: uint32(cols), Rows: uint32(rows)},
			}}); err != nil {
				return err
			}

			// raw mode so keystrokes pass straight through
			if term.IsTerminal(int(os.Stdin.Fd())) {
				if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
					defer func() { _ = term.Restore(int(os.Stdin.Fd()), old) }()
				}
			}
			code := pump(stream, os.Stdin, os.Stdout)
			if code != 0 {
				return errors.New("shell exited non-zero")
			}
			return nil
		},
	}
}
