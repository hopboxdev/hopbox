package users

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	fp := "SHA256_aa_bb_cc_dd"

	// Initially not found
	_, ok := store.LookupByFingerprint(fp)
	if ok {
		t.Fatal("expected user not found")
	}

	// Register
	u := User{
		Username:     "gandalf",
		KeyType:      "ed25519",
		RegisteredAt: time.Now().UTC().Truncate(time.Second),
	}
	err := store.Save(fp, u)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Lookup should work after reload
	store2 := NewStore(dir)
	got, ok := store2.LookupByFingerprint(fp)
	if !ok {
		t.Fatal("expected user found after reload")
	}
	if got.Username != "gandalf" {
		t.Errorf("username: got %q, want %q", got.Username, "gandalf")
	}
	if got.KeyType != "ed25519" {
		t.Errorf("key type: got %q, want %q", got.KeyType, "ed25519")
	}

	// Home dir should exist
	homeDir := filepath.Join(dir, fp, "home")
	if !dirExists(homeDir) {
		t.Errorf("expected home dir at %s", homeDir)
	}
}

func TestUsernameUniqueness(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	u := User{Username: "gandalf", KeyType: "ed25519", RegisteredAt: time.Now().UTC()}
	err := store.Save("SHA256_aa", u)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Same username, different fingerprint should fail
	err = store.Save("SHA256_bb", u)
	if err == nil {
		t.Fatal("expected error for duplicate username")
	}
}

func TestFingerprintFormat(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SHA256:aa:bb:cc:dd", "SHA256_aa_bb_cc_dd"},
		{"SHA256:1oqjkkSmu/CY8iEziTJSGfY0ii66r0DKrv81SKH7vpE", "SHA256_1oqjkkSmu-CY8iEziTJSGfY0ii66r0DKrv81SKH7vpE"},
		{"SHA256:abc+def/ghi", "SHA256_abc-def-ghi"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := FormatFingerprint(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "/") {
				t.Errorf("fingerprint contains /: %q", got)
			}
		})
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestLinkKey_BothFingerprintsResolve(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a user
	originalFP := "SHA256_original"
	user := User{
		Username:     "testuser",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(originalFP, user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Link a new key
	newFP := "SHA256_newkey"
	if err := store.LinkKey(newFP, originalFP); err != nil {
		t.Fatalf("LinkKey: %v", err)
	}

	// Both fingerprints should resolve to the same user
	u1, ok := store.LookupByFingerprint(originalFP)
	if !ok {
		t.Fatal("original fingerprint not found")
	}
	u2, ok := store.LookupByFingerprint(newFP)
	if !ok {
		t.Fatal("new fingerprint not found after linking")
	}
	if u1.Username != u2.Username {
		t.Fatalf("usernames don't match: %q vs %q", u1.Username, u2.Username)
	}

	// Verify the symlink exists on disk
	linkPath := filepath.Join(dir, newFP)
	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink, got regular file/dir")
	}
}

func TestLinkKey_OriginalNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	err := store.LinkKey("SHA256_new", "SHA256_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent original fingerprint")
	}
}

func TestLinkKey_NewAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	fp1 := "SHA256_first"
	fp2 := "SHA256_second"
	user := User{
		Username:     "user1",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(fp1, user); err != nil {
		t.Fatalf("Save fp1: %v", err)
	}

	user2 := User{
		Username:     "user2",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(fp2, user2); err != nil {
		t.Fatalf("Save fp2: %v", err)
	}

	err := store.LinkKey(fp2, fp1)
	if err == nil {
		t.Fatal("expected error when new fingerprint dir already exists")
	}
}

func TestLinkKey_ReloadPicksUpLink(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	originalFP := "SHA256_orig"
	user := User{
		Username:     "reloaduser",
		KeyType:      "ssh-ed25519",
		RegisteredAt: time.Now().UTC(),
	}
	if err := store.Save(originalFP, user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	newFP := "SHA256_linked"
	if err := store.LinkKey(newFP, originalFP); err != nil {
		t.Fatalf("LinkKey: %v", err)
	}

	// Create a fresh store from the same directory to test load picks up symlinks
	store2 := NewStore(dir)
	u, ok := store2.LookupByFingerprint(newFP)
	if !ok {
		t.Fatal("linked fingerprint not found after reload")
	}
	if u.Username != "reloaduser" {
		t.Fatalf("expected username reloaduser, got %q", u.Username)
	}
}
