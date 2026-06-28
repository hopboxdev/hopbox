// Package account is the dev-env's registered-key directory — it backs the box
// persistence tier. A key in the directory is an account (its boxes are
// persistent, auto-suspending); an unknown key is anonymous (its boxes are
// ephemeral). It implements sshfront.Authority (key -> principal) and provides
// IsAccount for the box engine's tier policy, so both consult one source.
package account

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Directory resolves SSH public keys to account principals.
type Directory struct {
	byFP     map[string]string // key fingerprint -> account principal
	accounts map[string]bool   // the set of account principals
}

// Load parses a registered-keys file: lines of `<ssh-public-key> <account>` (the
// account is the key comment), with '#' comments and blanks ignored. e.g.
//
//	ssh-ed25519 AAAA... alice
func Load(path string) (*Directory, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := &Directory{byFP: map[string]string{}, accounts: map[string]bool{}}
	sc := bufio.NewScanner(f)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, ln, err)
		}
		account := strings.TrimSpace(comment)
		if account == "" {
			return nil, fmt.Errorf("%s:%d: key has no account (trailing comment)", path, ln)
		}
		d.byFP[ssh.FingerprintSHA256(key)] = account
		d.accounts[account] = true
	}
	return d, sc.Err()
}

// Len reports how many keys are registered.
func (d *Directory) Len() int { return len(d.byFP) }

// Authenticate (sshfront.Authority) resolves a key to its account principal, or
// to the key fingerprint when the key is unregistered (anonymous).
func (d *Directory) Authenticate(key ssh.PublicKey) (string, error) {
	if a, ok := d.byFP[ssh.FingerprintSHA256(key)]; ok {
		return a, nil
	}
	return ssh.FingerprintSHA256(key), nil
}

// IsAccount reports whether a principal is a registered account — the box
// engine's tier policy: accounts get persistent boxes, anonymous fingerprints
// get ephemeral ones.
func (d *Directory) IsAccount(principal string) bool { return d.accounts[principal] }
