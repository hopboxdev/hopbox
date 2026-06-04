// Package config parses mesad's flags/env into a Config struct.
package config

import "flag"

type Config struct {
	APIAddr        string // gRPC API listen (CLI clients)
	AgentListen    string // where agents dial in
	AgentAdvertise string // address agents are told to dial (reachable from inside containers)
	DBPath         string
	AgentBin       string // host path of the linux mesa-agent binary to side-load
	Tenant         string
	Owner          string
}

func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("mesad", flag.ContinueOnError)
	var c Config
	fs.StringVar(&c.APIAddr, "api-addr", ":7700", "gRPC API listen address")
	fs.StringVar(&c.AgentListen, "agent-listen", ":7777", "agent reverse-dial listen address")
	fs.StringVar(&c.AgentAdvertise, "agent-advertise", "host.docker.internal:7777", "address agents dial back to")
	fs.StringVar(&c.DBPath, "db", "./mesa.db", "sqlite database path")
	fs.StringVar(&c.AgentBin, "agent-bin", "./bin/mesa-agent-linux-amd64", "mesa-agent binary to side-load")
	fs.StringVar(&c.Tenant, "tenant", "default", "single-tenant id (M1)")
	fs.StringVar(&c.Owner, "owner", "dev", "single principal (M1)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
