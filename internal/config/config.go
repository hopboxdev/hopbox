package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Port             int    `toml:"port"`
	DataDir          string `toml:"data_dir"`
	HostKeyPath      string `toml:"host_key_path"`
	OpenRegistration bool   `toml:"open_registration"`
}

func defaults() Config {
	return Config{
		Port:             2222,
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
	}
}

// Load reads config from path. If path is empty, tries ./config.toml.
// If the file doesn't exist, returns defaults.
func Load(path string) (Config, error) {
	cfg := defaults()

	if path == "" {
		path = "config.toml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
