package users

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type User struct {
	Username     string    `toml:"username"`
	KeyType      string    `toml:"key_type"`
	RegisteredAt time.Time `toml:"registered_at"`
}

type Store struct {
	mu    sync.RWMutex
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
		// Use os.Stat (follows symlinks) rather than e.IsDir() to handle symlinked dirs
		info, err := os.Stat(filepath.Join(s.dir, e.Name()))
		if err != nil || !info.IsDir() {
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[fp]
	return u, ok
}

// IsUsernameTaken checks if a username is already registered by any user.
func (s *Store) IsUsernameTaken(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == name {
			return true
		}
	}
	return false
}

func (s *Store) Save(fp string, u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// Dir returns the base directory of the store.
func (s *Store) Dir() string {
	return s.dir
}

// HomePath returns the path to the user's bind-mounted home directory for a given boxname.
func (s *Store) HomePath(fp, boxname string) string {
	return filepath.Join(s.dir, fp, "boxes", boxname, "home")
}

// ListAll returns a copy of all users keyed by fingerprint.
func (s *Store) ListAll() map[string]User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]User, len(s.users))
	for fp, u := range s.users {
		out[fp] = u
	}
	return out
}

// Delete removes a user by fingerprint from the in-memory map and deletes their directory.
func (s *Store) Delete(fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[fp]; !ok {
		return fmt.Errorf("user not found: %s", fp)
	}

	userDir := filepath.Join(s.dir, fp)
	if err := os.RemoveAll(userDir); err != nil {
		return fmt.Errorf("remove user dir: %w", err)
	}

	delete(s.users, fp)
	return nil
}

// LinkKey creates a symlink so that newFP resolves to the same user as originalFP.
// After linking, both fingerprints will return the same User from LookupByFingerprint.
func (s *Store) LinkKey(newFP, originalFP string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the original fingerprint exists
	if _, ok := s.users[originalFP]; !ok {
		return fmt.Errorf("original fingerprint not found: %s", originalFP)
	}

	newDir := filepath.Join(s.dir, newFP)

	// Check the new fingerprint dir doesn't already exist
	if _, err := os.Lstat(newDir); err == nil {
		return fmt.Errorf("fingerprint directory already exists: %s", newFP)
	}

	// Create the symlink: newFP -> originalFP (relative so it works if data dir moves)
	if err := os.Symlink(originalFP, newDir); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	// Update the in-memory map for the new fingerprint
	s.users[newFP] = s.users[originalFP]

	return nil
}

// FormatFingerprint converts an SSH fingerprint to a filesystem-safe directory name.
// Replaces colons with underscores and slashes with dashes (base64 fingerprints contain /).
func FormatFingerprint(raw string) string {
	s := strings.ReplaceAll(raw, ":", "_")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "+", "-")
	return s
}
