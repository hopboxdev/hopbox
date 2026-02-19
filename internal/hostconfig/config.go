package hostconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// HostConfig holds all connection parameters for a remote hop host.
// Stored at ~/.config/hopbox/hosts/<name>.yaml.
type HostConfig struct {
	Name          string `yaml:"name"`
	Endpoint      string `yaml:"endpoint"`        // "host:port" UDP
	PrivateKey    string `yaml:"private_key"`     // client private key, base64
	PeerPublicKey string `yaml:"peer_public_key"` // server WG public key, base64
	TunnelIP      string `yaml:"tunnel_ip"`       // client WG IP, CIDR
	AgentIP       string `yaml:"agent_ip"`        // server WG IP, plain
	SSHUser       string `yaml:"ssh_user"`
	SSHHost       string `yaml:"ssh_host"`
	SSHPort       int    `yaml:"ssh_port"`
	// SSHHostKey is the server's SSH host key in authorized_keys format,
	// captured during `hop setup` and verified on all subsequent SSH connections.
	SSHHostKey string `yaml:"ssh_host_key,omitempty"`
}

// ConfigDir returns the directory where host configs are stored.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox", "hosts"), nil
}

// Save writes the config to ~/.config/hopbox/hosts/<name>.yaml.
func (c *HostConfig) Save() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, c.Name+".yaml")
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// Load reads a host config by name.
func Load(name string) (*HostConfig, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host config %q: %w", name, err)
	}
	var cfg HostConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse host config %q: %w", name, err)
	}
	return &cfg, nil
}

// List returns the names of all saved host configs.
func List() ([]string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list host configs: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	return names, nil
}

// Delete removes a host config by name.
func Delete(name string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".yaml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete host config %q: %w", name, err)
	}
	return nil
}

// ToTunnelConfig converts this host config to a tunnel.Config for the client.
func (c *HostConfig) ToTunnelConfig() tunnel.Config {
	agentIP := c.AgentIP
	if !strings.Contains(agentIP, "/") {
		agentIP += "/32"
	}
	return tunnel.Config{
		PrivateKey:          c.PrivateKey,
		PeerPublicKey:       c.PeerPublicKey,
		LocalIP:             c.TunnelIP,
		PeerIP:              agentIP,
		Endpoint:            c.Endpoint,
		ListenPort:          0,
		MTU:                 tunnel.DefaultMTU,
		PersistentKeepalive: tunnel.DefaultKeepalive,
	}
}
