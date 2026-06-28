package account

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func mustKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

func authLine(key ssh.PublicKey, account string) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " " + account + "\n"
}

func TestDirectoryTiers(t *testing.T) {
	known, unknown := mustKey(t), mustKey(t)
	file := filepath.Join(t.TempDir(), "accounts")
	if err := os.WriteFile(file, []byte("# registered\n"+authLine(known, "alice")), 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if d.Len() != 1 {
		t.Fatalf("len=%d want 1", d.Len())
	}

	// registered key -> account principal, which is persistent-eligible.
	if p, _ := d.Authenticate(known); p != "alice" {
		t.Fatalf("known principal=%q want alice", p)
	}
	if !d.IsAccount("alice") {
		t.Fatal("alice should be a registered account")
	}

	// unknown key -> anonymous fingerprint, not an account.
	p, _ := d.Authenticate(unknown)
	if p != ssh.FingerprintSHA256(unknown) {
		t.Fatalf("unknown principal=%q want fingerprint", p)
	}
	if d.IsAccount(p) {
		t.Fatal("anonymous fingerprint must not be an account")
	}
}
