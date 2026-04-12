package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

type CLI struct {
	Init     InitCmd     `cmd:"" help:"Set up hop configuration."`
	SSH      SSHCmd      `cmd:"" help:"Open an SSH session to your box."`
	Expose   ExposeCmd   `cmd:"" help:"Forward a port from your box to localhost."`
	Transfer TransferCmd `cmd:"" help:"Upload a file to your box."`
	Config   ConfigCmd   `cmd:"" help:"Print resolved configuration."`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("hop"),
		kong.Description("Hopbox client CLI"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	switch ctx.Command() {
	case "":
		cfg, err := loadConfig(configPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cfg.applyEnv()
		if cfg.Server == "" || cfg.User == "" {
			ctx.PrintUsage(false)
			os.Exit(0)
		}
		sshCmd := SSHCmd{}
		if err := sshCmd.Run(&cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		if err := ctx.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
