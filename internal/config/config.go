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

	AgentImageRef    string
	AgentBinaryPath  string
	AgentTargetPath  string
	ComputeKind      string
	ComputeTransport string
	ComputeRemote    string
	StorageKind      string
	StorageTransport string
	StorageRemote    string
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
	fs.StringVar(&c.AgentImageRef, "agent-image", "", "OCI image carrying the mesa-agent binary")
	fs.StringVar(&c.AgentBinaryPath, "agent-binary-path", "/mesa-agent", "agent binary path inside the agent image")
	fs.StringVar(&c.AgentTargetPath, "agent-target-path", "/mesa/mesa-agent", "where to place+run the agent in the workspace")
	fs.StringVar(&c.ComputeKind, "compute", "docker", "compute provider: docker|kubernetes")
	fs.StringVar(&c.ComputeTransport, "compute-transport", "inproc", "compute transport: inproc|remote")
	fs.StringVar(&c.ComputeRemote, "compute-remote", "", "remote compute provider address (when --compute-transport=remote)")
	fs.StringVar(&c.StorageKind, "storage", "localfs", "storage provider: localfs|k8spvc")
	fs.StringVar(&c.StorageTransport, "storage-transport", "inproc", "storage transport: inproc|remote")
	fs.StringVar(&c.StorageRemote, "storage-remote", "", "remote storage provider address")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
