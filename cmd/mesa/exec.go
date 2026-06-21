package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	mesav1 "github.com/mesadev/mesa/gen/mesa/v1"
)

// newExecCmd runs a non-interactive command in a workspace, streaming
// stdout/stderr to the terminal and exiting with the command's exit code.
func newExecCmd(dial func() (mesav1.WorkspaceServiceClient, func(), error)) *cobra.Command {
	c := &cobra.Command{
		Use:   "exec <name|id> [--] <command>...",
		Short: "Run a command in a workspace (non-interactive)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			name, command := args[0], args[1:]
			// With SetInterspersed(false) pflag keeps a literal "--" separator;
			// drop it so `mesa exec web -- cmd` and `mesa exec web cmd` both work.
			if len(command) > 0 && command[0] == "--" {
				command = command[1:]
			}
			if len(command) == 0 {
				return fmt.Errorf("a command is required")
			}
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()

			stream, err := client.Exec(context.Background(), &mesav1.ExecRequest{NameOrId: name, Cmd: command})
			if err != nil {
				return err
			}
			code := 0
			for {
				msg, rerr := stream.Recv()
				if rerr == io.EOF {
					break
				}
				if rerr != nil {
					return rerr
				}
				if d := msg.GetStdout(); d != nil {
					_, _ = os.Stdout.Write(d)
				}
				if d := msg.GetStderr(); d != nil {
					_, _ = os.Stderr.Write(d)
				}
				if _, ok := msg.Msg.(*mesav1.ExecServerMsg_ExitCode); ok {
					code = int(msg.GetExitCode())
				}
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	// Treat everything after the workspace name as the command (don't parse its
	// flags as mesa flags), so `mesa exec web ls -la` works without a `--`.
	c.Flags().SetInterspersed(false)
	return c
}
