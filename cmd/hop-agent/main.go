package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/hopboxdev/hopbox/internal/agent"
	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/packages"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/version"
	"github.com/hopboxdev/hopbox/internal/wgkey"
)

const agentKeyFile = "/etc/hopbox/agent.key"

// ServeCmd starts the hop-agent daemon.
type ServeCmd struct {
	Workspace string `name:"workspace" short:"w" help:"Path to hopbox.yaml to load on startup." type:"path"`
}

func (c *ServeCmd) Run() error {
	// Ensure static package binaries are on PATH for scripts and checks.
	_ = os.Setenv("PATH", packages.StaticBinDir+":"+os.Getenv("PATH"))

	kp, err := loadOrGenerateKey()
	if err != nil {
		return fmt.Errorf("load agent key: %w", err)
	}

	// Load peer public key from config file if present.
	peerPubKey := loadPeerPubKey()

	cfg := tunnel.Config{
		PrivateKey:    kp.PrivateKeyHex(),
		PeerPublicKey: peerPubKey,
		LocalIP:       tunnel.ServerIP + "/24",
		PeerIP:        tunnel.ClientIP + "/32",
		ListenPort:    tunnel.DefaultPort,
		MTU:           tunnel.DefaultMTU,
	}

	a := agent.New(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load workspace manifest if provided or found at the default location.
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "/etc/hopbox/hopbox.yaml"
	}
	if _, err := os.Stat(wsPath); err == nil {
		ws, err := manifest.Parse(wsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse manifest %s: %v\n", wsPath, err)
		} else {
			mgr := agent.BuildServiceManager(ws)
			a.WithServices(mgr)
			if len(ws.Scripts) > 0 {
				a.WithScripts(ws.Scripts)
			}
			if ws.Backup != nil && ws.Backup.Target != "" {
				a.WithBackupConfig(ws.Backup.Target, mgr.DataPaths())
			}
			a.WithManifestPath(wsPath)
			go func() {
				if err := mgr.StartAll(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: service startup: %v\n", err)
				}
			}()
			fmt.Printf("Loaded workspace: %s\n", ws.Name)
		}
	}

	fmt.Printf("hop-agent %s starting\n", version.Version)
	fmt.Printf("WireGuard IP: %s, listening on :%d\n", tunnel.ServerIP, tunnel.DefaultPort)
	fmt.Printf("Control API: %s:%d\n", tunnel.ServerIP, tunnel.AgentAPIPort)

	return a.Run(ctx)
}

// AgentSetupCmd configures the agent during bootstrap.
// Phase 1 (no flags): generate keys, print public key.
// Phase 2 (--client-pubkey): store client pubkey, complete WG config.
type AgentSetupCmd struct {
	ClientPubKey string `name:"client-pubkey" help:"Client WireGuard public key (base64). If set, configures the peer."`
}

func (c *AgentSetupCmd) Run() error {
	if c.ClientPubKey == "" {
		// Phase 1: generate or load key, print public key.
		kp, err := loadOrGenerateKey()
		if err != nil {
			return fmt.Errorf("generate key: %w", err)
		}
		fmt.Print(kp.PublicKeyBase64())
		return nil
	}

	// Phase 2: store client public key.
	if err := savePeerPubKey(c.ClientPubKey); err != nil {
		return fmt.Errorf("save peer pubkey: %w", err)
	}
	fmt.Println("ok")
	return nil
}

// AgentRotateCmd regenerates the server WireGuard keypair during key rotation.
// The old key is backed up to agent.key.bak before the new one is written.
type AgentRotateCmd struct{}

func (c *AgentRotateCmd) Run() error {
	_ = os.Rename(agentKeyFile, agentKeyFile+".bak")
	kp, err := wgkey.Generate()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err = kp.SaveToFile(agentKeyFile); err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	fmt.Print(kp.PublicKeyBase64())
	return nil
}

// VersionCmd prints version info.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("hop-agent %s (commit %s, built %s)\n",
		version.Version, version.Commit, version.Date)
	return nil
}

var cli struct {
	Serve   ServeCmd       `cmd:"" help:"Start the hop-agent daemon."`
	Setup   AgentSetupCmd  `cmd:"" help:"Configure agent during bootstrap."`
	Rotate  AgentRotateCmd `cmd:"" help:"Regenerate WireGuard keypair (backs up old key to agent.key.bak)."`
	Version VersionCmd     `cmd:"" help:"Print version."`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("hop-agent"),
		kong.Description("Hopbox agent — runs on your VPS"),
		kong.UsageOnError(),
	)
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}

// loadOrGenerateKey loads the agent key from agentKeyFile, or generates a new one.
func loadOrGenerateKey() (*wgkey.KeyPair, error) {
	kp, err := wgkey.LoadFromFile(agentKeyFile)
	if err == nil {
		return kp, nil
	}
	// Generate new key
	kp, err = wgkey.Generate()
	if err != nil {
		return nil, err
	}
	if err := kp.SaveToFile(agentKeyFile); err != nil {
		// Non-fatal if we can't persist (e.g. in tests)
		fmt.Fprintf(os.Stderr, "Warning: could not save key to %s: %v\n", agentKeyFile, err)
	}
	return kp, nil
}

const peerPubKeyFile = "/etc/hopbox/peer.pub"

func loadPeerPubKey() string {
	data, err := os.ReadFile(peerPubKeyFile)
	if err != nil {
		return ""
	}
	// Convert base64 to hex for IPC
	b64 := string(data)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		return ""
	}
	return fmt.Sprintf("%x", raw)
}

func savePeerPubKey(b64 string) error {
	if err := os.MkdirAll(filepath.Dir(peerPubKeyFile), 0700); err != nil {
		return err
	}
	return os.WriteFile(peerPubKeyFile, []byte(b64), 0600)
}
