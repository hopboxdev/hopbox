package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
)

// newProxyCmd is the plumbing behind native SSH: it bridges stdin/stdout to a
// workspace's embedded SSH server over the control plane. It is meant to be used
// as an OpenSSH ProxyCommand —
//
//	Host myhopbox
//	  ProxyCommand hopbox proxy %h
//
// so `ssh myhopbox`, VS Code "Connect to Host", scp, rsync and friends all work
// with no public port and no extra steps.
func newProxyCmd(dial func() (hopboxv1.WorkspaceServiceClient, func(), error)) *cobra.Command {
	c := &cobra.Command{
		Use:           "proxy <name|id>",
		Short:         "Stdio SSH transport to a workspace (use as an SSH ProxyCommand)",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(_ *cobra.Command, args []string) error {
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := client.SSH(ctx)
			if err != nil {
				return err
			}
			if err := stream.Send(&hopboxv1.SSHClientMsg{
				Msg: &hopboxv1.SSHClientMsg_Open{Open: &hopboxv1.SSHOpen{NameOrId: args[0]}},
			}); err != nil {
				return err
			}

			errc := make(chan error, 2)
			// stdin -> control plane (SSH client bytes)
			go func() {
				buf := make([]byte, 32*1024)
				for {
					n, rerr := os.Stdin.Read(buf)
					if n > 0 {
						if serr := stream.Send(&hopboxv1.SSHClientMsg{
							Msg: &hopboxv1.SSHClientMsg_Data{Data: append([]byte(nil), buf[:n]...)},
						}); serr != nil {
							errc <- serr
							return
						}
					}
					if rerr != nil {
						_ = stream.CloseSend()
						errc <- rerr
						return
					}
				}
			}()
			// control plane -> stdout (SSH server bytes)
			go func() {
				for {
					msg, rerr := stream.Recv()
					if rerr != nil {
						errc <- rerr
						return
					}
					if d := msg.GetData(); len(d) > 0 {
						if _, werr := os.Stdout.Write(d); werr != nil {
							errc <- werr
							return
						}
					}
				}
			}()

			err = <-errc
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		},
	}
	return c
}
