// Package config parses hopboxd's flags/env into a Config struct.
package config

import (
	"flag"
	"runtime"
)

type Config struct {
	APIAddr        string // gRPC API listen (CLI clients)
	AgentListen    string // where agents dial in
	AgentAdvertise string // address agents are told to dial (reachable from inside containers)
	DBPath         string
	AgentBin       string // host path of the linux hopbox-agent binary to side-load
	Tenant         string
	Owner          string

	UsersFile          string // token->principal map enabling multi-user auth; empty = open single-user mode
	SSHCAPath          string // path to the SSH user CA private key (created on first run); workspaces trust its public key
	AuthorizedKeysFile string // fallback static authorized_keys file injected into workspaces (no-login mode)

	AgentImageRef    string
	AgentBinaryPath  string
	AgentTargetPath  string
	ComputeKind      string
	ComputeTransport string
	ComputeRemote    string
	StorageKind      string
	StorageTransport string
	StorageRemote    string

	KubeNamespace    string
	Kubeconfig       string
	KubeStorageClass string
	KubeHomeSize     string

	GatewayAddr string
	GatewayZone string
	TunnelAddr  string
}

func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("hopboxd", flag.ContinueOnError)
	var c Config
	fs.StringVar(&c.APIAddr, "api-addr", ":7700", "gRPC API listen address")
	fs.StringVar(&c.AgentListen, "agent-listen", ":7777", "agent reverse-dial listen address")
	fs.StringVar(&c.AgentAdvertise, "agent-advertise", "host.docker.internal:7777", "address agents dial back to")
	fs.StringVar(&c.DBPath, "db", "./hopbox.db", "sqlite database path")
	// Default to the host-arch agent: the binary is injected and exec'd inside the
	// workspace container, which the docker provider pins to the host arch.
	fs.StringVar(&c.AgentBin, "agent-bin", "./bin/hopbox-agent-linux-"+runtime.GOARCH, "hopbox-agent binary to side-load")
	fs.StringVar(&c.Tenant, "tenant", "default", "single-tenant id (M1)")
	fs.StringVar(&c.Owner, "owner", "dev", "single principal (M1)")
	fs.StringVar(&c.UsersFile, "users", "", "token->principal file enabling multi-user auth (lines: `<token> <principal>`); empty = open single-user mode")
	fs.StringVar(&c.SSHCAPath, "ssh-ca", "./hopbox-ssh-ca", "SSH user-CA private key path (auto-created); workspaces trust its public key for `hopbox login` certs")
	fs.StringVar(&c.AuthorizedKeysFile, "authorized-keys", "", "fallback authorized_keys file injected into workspaces (no-login single-user mode)")
	fs.StringVar(&c.AgentImageRef, "agent-image", "", "OCI image carrying the hopbox-agent binary")
	fs.StringVar(&c.AgentBinaryPath, "agent-binary-path", "/hopbox-agent", "agent binary path inside the agent image")
	fs.StringVar(&c.AgentTargetPath, "agent-target-path", "/hopbox/hopbox-agent", "where to place+run the agent in the workspace")
	fs.StringVar(&c.ComputeKind, "compute", "docker", "compute provider: docker|kubernetes")
	fs.StringVar(&c.ComputeTransport, "compute-transport", "inproc", "compute transport: inproc|remote")
	fs.StringVar(&c.ComputeRemote, "compute-remote", "", "remote compute provider address (when --compute-transport=remote)")
	fs.StringVar(&c.StorageKind, "storage", "localfs", "storage provider: localfs|k8spvc")
	fs.StringVar(&c.StorageTransport, "storage-transport", "inproc", "storage transport: inproc|remote")
	fs.StringVar(&c.StorageRemote, "storage-remote", "", "remote storage provider address")
	fs.StringVar(&c.KubeNamespace, "kube-namespace", "hopbox-workspaces", "namespace for workspace pods/PVCs (kubernetes provider)")
	fs.StringVar(&c.Kubeconfig, "kubeconfig", "", "path to kubeconfig; empty = in-cluster config (kubernetes provider)")
	fs.StringVar(&c.KubeStorageClass, "kube-storageclass", "", "PVC StorageClass; empty = cluster default (k8spvc storage)")
	fs.StringVar(&c.KubeHomeSize, "kube-home-size", "1Gi", "PVC size for a workspace home (k8spvc storage)")
	fs.StringVar(&c.GatewayAddr, "gateway-addr", ":8088", "service gateway (hopbox-gw) HTTP listen address; empty disables")
	fs.StringVar(&c.GatewayZone, "gateway-zone", "gw.example.com", "wildcard DNS zone for the subdomain ingress provider")
	fs.StringVar(&c.TunnelAddr, "tunnel-addr", ":7701", "gateway tunnel listen address for standalone hopbox-gw; empty disables")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
