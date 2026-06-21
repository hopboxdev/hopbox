package plugin

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mesadev/mesa/internal/core/ports"
)

// ProviderConfig selects how a provider is loaded. The concrete in-process
// provider (which may import an SDK behind a build tag) is constructed by the
// caller and passed to Load*; the loader only adds the remote transport, so
// this package stays SDK-free.
type ProviderConfig struct {
	Kind       string // informational: "docker" | "kubernetes" | ...
	Transport  string // "inproc" | "remote"
	RemoteAddr string // host:port when Transport == "remote"
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// LoadCompute returns a ports.Compute for the configured transport. For inproc,
// it returns the provided in-process provider. For remote, it dials RemoteAddr
// (inproc may be nil).
func LoadCompute(cfg ProviderConfig, inproc ports.Compute) (ports.Compute, error) {
	switch cfg.Transport {
	case "", "inproc":
		if inproc == nil {
			return nil, fmt.Errorf("plugin: inproc compute provider is nil (build with the provider's tag, e.g. -tags docker)")
		}
		return inproc, nil
	case "remote":
		if cfg.RemoteAddr == "" {
			return nil, fmt.Errorf("plugin: remote compute transport requires RemoteAddr")
		}
		conn, err := dial(cfg.RemoteAddr)
		if err != nil {
			return nil, fmt.Errorf("plugin: dial compute %q: %w", cfg.RemoteAddr, err)
		}
		return NewRemoteCompute(conn), nil
	default:
		return nil, fmt.Errorf("plugin: unknown compute transport %q", cfg.Transport)
	}
}

// LoadStorage mirrors LoadCompute for ports.Storage.
func LoadStorage(cfg ProviderConfig, inproc ports.Storage) (ports.Storage, error) {
	switch cfg.Transport {
	case "", "inproc":
		if inproc == nil {
			return nil, fmt.Errorf("plugin: inproc storage provider is nil")
		}
		return inproc, nil
	case "remote":
		if cfg.RemoteAddr == "" {
			return nil, fmt.Errorf("plugin: remote storage transport requires RemoteAddr")
		}
		conn, err := dial(cfg.RemoteAddr)
		if err != nil {
			return nil, fmt.Errorf("plugin: dial storage %q: %w", cfg.RemoteAddr, err)
		}
		return NewRemoteStorage(conn), nil
	default:
		return nil, fmt.Errorf("plugin: unknown storage transport %q", cfg.Transport)
	}
}

// LoadIngress mirrors LoadCompute for ports.Ingress.
func LoadIngress(cfg ProviderConfig, inproc ports.Ingress) (ports.Ingress, error) {
	switch cfg.Transport {
	case "", "inproc":
		if inproc == nil {
			return nil, fmt.Errorf("plugin: inproc ingress provider is nil")
		}
		return inproc, nil
	case "remote":
		if cfg.RemoteAddr == "" {
			return nil, fmt.Errorf("plugin: remote ingress transport requires RemoteAddr")
		}
		conn, err := dial(cfg.RemoteAddr)
		if err != nil {
			return nil, fmt.Errorf("plugin: dial ingress %q: %w", cfg.RemoteAddr, err)
		}
		return NewRemoteIngress(conn), nil
	default:
		return nil, fmt.Errorf("plugin: unknown ingress transport %q", cfg.Transport)
	}
}

// LoadIdentity mirrors LoadCompute for ports.Identity.
func LoadIdentity(cfg ProviderConfig, inproc ports.Identity) (ports.Identity, error) {
	switch cfg.Transport {
	case "", "inproc":
		if inproc == nil {
			return nil, fmt.Errorf("plugin: inproc identity provider is nil")
		}
		return inproc, nil
	case "remote":
		if cfg.RemoteAddr == "" {
			return nil, fmt.Errorf("plugin: remote identity transport requires RemoteAddr")
		}
		conn, err := dial(cfg.RemoteAddr)
		if err != nil {
			return nil, fmt.Errorf("plugin: dial identity %q: %w", cfg.RemoteAddr, err)
		}
		return NewRemoteIdentity(conn), nil
	default:
		return nil, fmt.Errorf("plugin: unknown identity transport %q", cfg.Transport)
	}
}
