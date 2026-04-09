package users

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type MultiplexerConfig struct {
	Tool string `toml:"tool"`
}

type EditorConfig struct {
	Tool string `toml:"tool"`
}

type ShellConfig struct {
	Tool string `toml:"tool"`
}

type RuntimesConfig struct {
	Node   string `toml:"node"`
	Python string `toml:"python"`
	Go     string `toml:"go"`
	Rust   string `toml:"rust"`
}

type ToolsConfig struct {
	Extras []string `toml:"extras"`
}

type Profile struct {
	Multiplexer MultiplexerConfig `toml:"multiplexer"`
	Editor      EditorConfig      `toml:"editor"`
	Shell       ShellConfig       `toml:"shell"`
	Runtimes    RuntimesConfig    `toml:"runtimes"`
	Tools       ToolsConfig       `toml:"tools"`
}

func DefaultProfile() Profile {
	return Profile{
		Multiplexer: MultiplexerConfig{Tool: "zellij"},
		Editor:      EditorConfig{Tool: "neovim"},
		Shell:       ShellConfig{Tool: "bash"},
		Runtimes: RuntimesConfig{
			Node:   "lts",
			Python: "3.12",
			Go:     "none",
			Rust:   "none",
		},
		Tools: ToolsConfig{
			Extras: []string{"fzf", "ripgrep", "fd", "bat", "lazygit"},
		},
	}
}

func SaveProfile(path string, p Profile) error {
	data, err := toml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadProfile(path string) (Profile, error) {
	var p Profile
	data, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	return p, toml.Unmarshal(data, &p)
}

func (p Profile) Hash() string {
	data, _ := toml.Marshal(p)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:12]
}

func ResolveProfile(userDir, boxname string) (*Profile, error) {
	// Try box-level first
	boxPath := filepath.Join(userDir, "boxes", boxname, "profile.toml")
	if p, err := LoadProfile(boxPath); err == nil {
		return &p, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	// Fall back to user-level
	userPath := filepath.Join(userDir, "profile.toml")
	if p, err := LoadProfile(userPath); err == nil {
		return &p, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	return nil, nil
}
