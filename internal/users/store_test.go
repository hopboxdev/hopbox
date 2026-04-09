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
