package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type ResourcesConfig struct {
	CPUCores  int   `toml:"cpu_cores"`
	MemoryGB  int   `toml:"memory_gb"`
	PidsLimit int64 `toml:"pids_limit"`
}

type AdminConfig struct {
	Enabled  bool   `toml:"enabled"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type Config struct {
	Port             int             `toml:"port"`
	Hostname         string          `toml:"hostname"`
	DataDir          string          `toml:"data_dir"`
	HostKeyPath      string          `toml:"host_key_path"`
	OpenRegistration bool            `toml:"open_registration"`
	IdleTimeoutHours int             `toml:"idle_timeout_hours"`
	LogFormat        string          `toml:"log_format"`
	LogLevel         string          `toml:"log_level"`
	ContainerRuntime string          `toml:"container_runtime"`
	Resources        ResourcesConfig `toml:"resources"`
	Admin            AdminConfig     `toml:"admin"`
}

func defaults() Config {
	return Config{
		Port:             2222,
		Hostname:         "",
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
		IdleTimeoutHours: 24,
		LogFormat:        "text",
		LogLevel:         "info",
		Resources: ResourcesConfig{
			CPUCores:  2,
			MemoryGB:  4,
			PidsLimit: 512,
		},
		Admin: AdminConfig{
			Enabled:  false,
			Port:     8080,
			Username: "admin",
			Password: "",
		},
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

// LogValue implements slog.LogValuer so the full config is logged as a
// single JSON attribute. Any field added to Config automatically shows up
// in the log, and sensitive fields are redacted before serialisation.
func (c Config) LogValue() slog.Value {
	redacted := c
	if redacted.Admin.Password != "" {
		redacted.Admin.Password = "***"
	}
	data, err := json.Marshal(redacted)
	if err != nil {
		return slog.StringValue("<marshal error: " + err.Error() + ">")
	}
	return slog.StringValue(string(data))
}
