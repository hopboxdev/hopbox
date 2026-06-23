package agentssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// LoadOrCreateHostKey returns the ed25519 host key at path, generating and
// persisting one (0600) on first use so the host identity is stable across
// agent restarts and clients can pin it in known_hosts.
func LoadOrCreateHostKey(path string) (ssh.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("agentssh: parse host key %s: %w", path, err)
		}
		return signer, nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("agentssh: write host key %s: %w", path, err)
	}
	return ssh.NewSignerFromSigner(priv)
}

// ParseAuthorizedKeys parses an OpenSSH authorized_keys blob into public keys,
// ignoring blank lines and comments.
func ParseAuthorizedKeys(b []byte) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	rest := b
	for len(rest) > 0 {
		key, _, _, r, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			// no more parseable keys
			break
		}
		keys = append(keys, key)
		rest = r
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("agentssh: no public keys found")
	}
	return keys, nil
}
