package hostconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/wgkey"
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
	// SSHKeyPath is the path to the private key used during hop setup.
	// Passed as -i to ssh in hop shell. Empty means use SSH default key discovery.
	SSHKeyPath string `yaml:"ssh_key_path,omitempty"`
	// SSHHostKey is the server's SSH host key in authorized_keys format,
	// captured during `hop setup` and verified on all subsequent SSH connections.
	SSHHostKey string `yaml:"ssh_host_key,omitempty"`
}

// GlobalConfig holds user-level settings for hop.
// Stored at ~/.config/hopbox/config.yaml.
type GlobalConfig struct {
	DefaultHost string `yaml:"default_host,omitempty"`
}

// validateName rejects host names that could escape the config directory.
// Only letters, digits, hyphens, and underscores are allowed; the first
// character must be a letter or digit.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("host name must not be empty")
	}
	for i, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		isDash := r == '-' || r == '_'
		switch {
		case isLetter || isDigit:
			// always allowed
		case isDash && i > 0:
			// allowed after the first character
		default:
			if i == 0 {
				return fmt.Errorf("host name %q must start with a letter or digit", name)
			}
			return fmt.Errorf("host name %q contains invalid character %q (only letters, digits, - and _ allowed)", name, string(r))
		}
	}
	return nil
}

// hopboxDir returns the base config directory (~/.config/hopbox).
func hopboxDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "hopbox"), nil
}

// ConfigDir returns the directory where host configs are stored.
func ConfigDir() (string, error) {
	base, err := hopboxDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "hosts"), nil
}

// LoadGlobalConfig reads ~/.config/hopbox/config.yaml.
// Returns an empty config (not an error) if the file does not exist yet.
func LoadGlobalConfig() (*GlobalConfig, error) {
	base, err := hopboxDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(base, "config.yaml"))
	if os.IsNotExist(err) {
		return &GlobalConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read global config: %w", err)
	}
	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse global config: %w", err)
	}
	return &cfg, nil
}

// Save writes the global config to ~/.config/hopbox/config.yaml.
func (c *GlobalConfig) Save() error {
	base, err := hopboxDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal global config: %w", err)
	}
	return os.WriteFile(filepath.Join(base, "config.yaml"), data, 0600)
}

// SetDefaultHost sets default_host in ~/.config/hopbox/config.yaml.
func SetDefaultHost(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	cfg.DefaultHost = name
	return cfg.Save()
}

// Save writes the config to ~/.config/hopbox/hosts/<name>.yaml.
func (c *HostConfig) Save() error {
	if err := validateName(c.Name); err != nil {
		return err
	}
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
	if err := validateName(name); err != nil {
		return nil, err
	}
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
	if err := validateName(name); err != nil {
		return err
	}
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
// Keys are converted from base64 (storage format) to hex (WireGuard IPC format).
func (c *HostConfig) ToTunnelConfig() (tunnel.Config, error) {
	privHex, err := wgkey.KeyB64ToHex(c.PrivateKey)
	if err != nil {
		return tunnel.Config{}, fmt.Errorf("convert private key: %w", err)
	}
	peerHex, err := wgkey.KeyB64ToHex(c.PeerPublicKey)
	if err != nil {
		return tunnel.Config{}, fmt.Errorf("convert peer public key: %w", err)
	}
	agentIP := c.AgentIP
	if !strings.Contains(agentIP, "/") {
		agentIP += "/32"
	}
	return tunnel.Config{
		PrivateKey:          privHex,
		PeerPublicKey:       peerHex,
		LocalIP:             c.TunnelIP,
		PeerIP:              agentIP,
		Endpoint:            c.Endpoint,
		ListenPort:          0,
		MTU:                 tunnel.DefaultMTU,
		PersistentKeepalive: tunnel.DefaultKeepalive,
	}, nil
}
