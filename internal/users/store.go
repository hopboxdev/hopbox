package users

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type User struct {
	Username     string    `toml:"username"`
	KeyType      string    `toml:"key_type"`
	RegisteredAt time.Time `toml:"registered_at"`
}

type Store struct {
	dir   string
	users map[string]User // fingerprint -> User
}

func NewStore(dir string) *Store {
	s := &Store{
		dir:   dir,
		users: make(map[string]User),
	}
	s.load()
	return s
}

func (s *Store) load() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fp := e.Name()
		path := filepath.Join(s.dir, fp, "user.toml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var u User
		if err := toml.Unmarshal(data, &u); err != nil {
			continue
		}
		s.users[fp] = u
	}
}

func (s *Store) LookupByFingerprint(fp string) (User, bool) {
	u, ok := s.users[fp]
	return u, ok
}

func (s *Store) Save(fp string, u User) error {
	// Check username uniqueness
	for existingFP, existing := range s.users {
		if existing.Username == u.Username && existingFP != fp {
			return fmt.Errorf("username %q already taken", u.Username)
		}
	}

	userDir := filepath.Join(s.dir, fp)
	homeDir := filepath.Join(userDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return fmt.Errorf("create user dirs: %w", err)
	}

	data, err := toml.Marshal(u)
	if err != nil {
		return fmt.Errorf("marshal user: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "user.toml"), data, 0644); err != nil {
		return fmt.Errorf("write user.toml: %w", err)
	}

	s.users[fp] = u
	return nil
}

// HomePath returns the path to the user's bind-mounted home directory.
func (s *Store) HomePath(fp string) string {
	return filepath.Join(s.dir, fp, "home")
}

// FormatFingerprint converts "SHA256:aa:bb:cc:dd" to "SHA256_aa_bb_cc_dd".
func FormatFingerprint(raw string) string {
	return strings.ReplaceAll(raw, ":", "_")
}
