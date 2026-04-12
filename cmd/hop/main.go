package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

type CLI struct {
	SSH      SSHCmd      `cmd:"" default:"withargs" help:"SSH into your box (default command)."`
	Init     InitCmd     `cmd:"" help:"Set up hop configuration."`
	Expose   ExposeCmd   `cmd:"" help:"Forward a port from your box to localhost."`
	Transfer TransferCmd `cmd:"" help:"Upload a file to your box."`
	Config   ConfigCmd   `cmd:"" help:"Print resolved configuration."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox client CLI — run with no command to SSH into your box."),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	if err := ctx.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
