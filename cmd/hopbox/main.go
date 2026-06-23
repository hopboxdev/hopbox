// Command hopbox is the Hopbox user CLI: create/ls/rm/shell against hopboxd.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	hopboxv1 "github.com/hopboxdev/hopbox/gen/hopbox/v1"
)

// parseExpose turns a "name:port" flag value into an IngressPort.
func parseExpose(s string) (*hopboxv1.IngressPort, error) {
	name, portStr, ok := strings.Cut(s, ":")
	if !ok || name == "" || portStr == "" {
		return nil, fmt.Errorf("invalid --expose %q, want name:port (e.g. app:3000)", s)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("invalid port in --expose %q", s)
	}
	return &hopboxv1.IngressPort{Name: name, Port: int32(port)}, nil
}

var apiAddr string

func dial() (hopboxv1.WorkspaceServiceClient, func(), error) {
	conn, err := grpc.NewClient(apiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return hopboxv1.NewWorkspaceServiceClient(conn), func() { _ = conn.Close() }, nil
}

func main() {
	root := &cobra.Command{Use: "hopbox", Short: "Hopbox dev-environment CLI"}
	root.PersistentFlags().StringVar(&apiAddr, "addr", "localhost:7700", "hopboxd API address")

	root.AddCommand(newCreateCmd(), newListCmd(), newRmCmd(), newShellCmd(dial), newExecCmd(dial), newProxyCmd(dial), newSSHConfigCmd(), newSSHCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newCreateCmd() *cobra.Command {
	var image string
	var mem int64
	var expose []string
	c := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var ingress []*hopboxv1.IngressPort
			for _, e := range expose {
				ip, err := parseExpose(e)
				if err != nil {
					return err
				}
				ingress = append(ingress, ip)
			}
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()
			w, err := client.CreateWorkspace(context.Background(), &hopboxv1.CreateWorkspaceRequest{
				Name: args[0], ImageRef: image, MemMb: mem, Ingress: ingress,
			})
			if err != nil {
				return err
			}
			fmt.Printf("created %s (%s) phase=%s\n", w.Name, w.Id, w.Phase)
			return nil
		},
	}
	c.Flags().StringVar(&image, "image", "ubuntu:24.04", "container image")
	c.Flags().Int64Var(&mem, "mem-mb", 0, "memory limit in MB (0=unlimited)")
	c.Flags().StringArrayVar(&expose, "expose", nil, "expose a workspace port at the gateway: name:port (repeatable)")
	return c
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List workspaces",
		RunE: func(_ *cobra.Command, _ []string) error {
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()
			resp, err := client.ListWorkspaces(context.Background(), &hopboxv1.ListWorkspacesRequest{})
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tPHASE\tAGENT\tIMAGE\tENDPOINTS\tID")
			for _, w := range resp.Workspaces {
				var urls []string
				for _, e := range w.Endpoints {
					urls = append(urls, e.Url)
				}
				eps := strings.Join(urls, ",")
				if eps == "" {
					eps = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\t%s\n", w.Name, w.Phase, w.AgentConnected, w.ImageRef, eps, w.Id)
			}
			return tw.Flush()
		},
	}
}

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name|id>",
		Short: "Destroy a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client, closer, err := dial()
			if err != nil {
				return err
			}
			defer closer()
			if _, err := client.DeleteWorkspace(context.Background(), &hopboxv1.DeleteWorkspaceRequest{NameOrId: args[0]}); err != nil {
				return err
			}
			fmt.Printf("destroying %s\n", args[0])
			return nil
		},
	}
}
