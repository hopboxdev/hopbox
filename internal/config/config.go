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
	SSHCAPubFile       string // trust an EXTERNAL SSH CA (public key); disables built-in issuance (enterprise)
	AuthorizedKeysFile string // fallback static authorized_keys file injected into workspaces (no-login mode)

	OIDCIssuer         string // OIDC issuer URL; set to authenticate via SSO instead of static tokens
	OIDCAudience       string // expected token audience (client id)
	OIDCPrincipalClaim string // claim used as the principal id: "sub" (default) or "email"
	OIDCAdminGroups    string // comma-separated groups granting the tenant-admin role

	AgentImageRef    string
	AgentBinaryPath  string
	AgentTargetPath  string
	ComputeKind      string
	ComputeTransport string
	ComputeRemote    string
	ComputeNetwork   string // docker: dedicated bridge for workspace boxes (isolates them); empty = default bridge
	FCBin            string // microvm: firecracker binary
	FCKernel         string // microvm: vmlinux kernel
	FCImagesDir      string // microvm: base-image catalog dir (<name>.ext4)
	FCRunDir         string // microvm: per-VM working dir
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

	EventsKind string // reconcile wake-up bus: inproc|nats
	NATSURL    string // NATS server URL when --events=nats

	SSHAddr         string  // krillbox-style SSH front-door listen (username=spec, key=identity); empty disables
	SSHHostKeyPath  string  // front-door SSH host key (auto-created on first run)
	SSHDefaultImage string  // image for front-door boxes when the username names none
	SSHDefaultMemMB int64   // memory cap (MB) for front-door boxes; 0 = unlimited
	SSHDefaultCPUs  float64 // CPU cap (vCPU) for front-door boxes; 0 = unlimited
	AccountsFile    string  // registered-keys file (`<ssh-key> <account>`): keys here get persistent boxes; empty = all anonymous/ephemeral
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
	fs.StringVar(&c.SSHCAPubFile, "ssh-ca-pub", "", "trust an external SSH CA public key instead of the built-in one (disables `hopbox login` issuance; use your own CA tooling)")
	fs.StringVar(&c.OIDCIssuer, "oidc-issuer", "", "OIDC issuer URL for SSO auth (e.g. https://accounts.google.com); overrides --users")
	fs.StringVar(&c.OIDCAudience, "oidc-audience", "", "expected OIDC token audience (client id)")
	fs.StringVar(&c.OIDCPrincipalClaim, "oidc-principal-claim", "sub", "OIDC claim used as the principal id: sub|email")
	fs.StringVar(&c.OIDCAdminGroups, "oidc-admin-groups", "", "comma-separated OIDC groups granted the tenant-admin role")
	fs.StringVar(&c.AuthorizedKeysFile, "authorized-keys", "", "fallback authorized_keys file injected into workspaces (no-login single-user mode)")
	fs.StringVar(&c.AgentImageRef, "agent-image", "", "OCI image carrying the hopbox-agent binary")
	fs.StringVar(&c.AgentBinaryPath, "agent-binary-path", "/hopbox-agent", "agent binary path inside the agent image")
	fs.StringVar(&c.AgentTargetPath, "agent-target-path", "/hopbox/hopbox-agent", "where to place+run the agent in the workspace")
	fs.StringVar(&c.ComputeKind, "compute", "docker", "compute provider: docker|microvm|kubernetes")
	fs.StringVar(&c.FCBin, "fc-bin", "/usr/local/bin/firecracker", "firecracker binary (microvm)")
	fs.StringVar(&c.FCKernel, "fc-kernel", "/opt/hopbox-microvm/vmlinux", "vmlinux kernel (microvm)")
	fs.StringVar(&c.FCImagesDir, "fc-images-dir", "/opt/hopbox-microvm/images", "base-image catalog dir (microvm)")
	fs.StringVar(&c.FCRunDir, "fc-rundir", "/var/lib/hopbox/microvm", "per-VM working dir (microvm)")
	fs.StringVar(&c.ComputeNetwork, "compute-network", "", "docker: put workspace boxes on this dedicated bridge to isolate them from the host's other containers; empty = default bridge")
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
	fs.StringVar(&c.EventsKind, "events", "inproc", "reconcile wake-up bus: inproc|nats (nats fans wake-ups across nodes)")
	fs.StringVar(&c.NATSURL, "nats-url", "nats://127.0.0.1:4222", "NATS server URL when --events=nats")
	fs.StringVar(&c.SSHAddr, "ssh-addr", "", "krillbox-style SSH front-door listen address (username=workspace spec, key=identity); empty disables")
	fs.StringVar(&c.SSHHostKeyPath, "ssh-host-key", "./hopbox-ssh-front-key", "front-door SSH host key path (auto-created)")
	fs.StringVar(&c.SSHDefaultImage, "ssh-default-image", "alpine", "image for front-door boxes when the username names none")
	fs.StringVar(&c.AccountsFile, "accounts", "", "registered-keys file (`<ssh-key> <account>`): listed keys get persistent boxes, others are anonymous/ephemeral")
	fs.Int64Var(&c.SSHDefaultMemMB, "ssh-default-mem-mb", 2048, "memory cap (MB) for front-door boxes (anonymous; capped to limit abuse); 0 = unlimited")
	fs.Float64Var(&c.SSHDefaultCPUs, "ssh-default-cpus", 2, "CPU cap (vCPU) for front-door boxes (anonymous; capped to limit abuse); 0 = unlimited")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
