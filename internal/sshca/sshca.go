// Package sshca is Hopbox's SSH certificate authority. A box trusts the CA's
// public key instead of a static authorized_keys list; users present a
// short-lived user certificate the CA signed for their principal. This is the
// model big fleets use to manage SSH access centrally (issue/expire, no per-box
// key distribution) and the simplest thing for a self-hoster (built-in CA,
// certs minted on login). The CA can be Hopbox's own or an external one whose
// public key the agents are told to trust.
package sshca

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// LoadOrCreateCA returns the ed25519 CA signer at path, generating and
// persisting one (0600) on first use so the CA identity is stable.
func LoadOrCreateCA(path string) (ssh.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("sshca: parse CA key %s: %w", path, err)
		}
		return signer, nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "hopbox-ssh-ca")
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("sshca: write CA key %s: %w", path, err)
	}
	return ssh.NewSignerFromSigner(priv)
}

// SignUserCert signs userPub into a user certificate valid for ttl, granting the
// given principals (e.g. the workspace owner). keyID is recorded for audit. The
// certificate permits an interactive pty; it carries no port-forwarding rights.
func SignUserCert(ca ssh.Signer, userPub ssh.PublicKey, keyID string, principals []string, ttl time.Duration) (*ssh.Certificate, error) {
	if ca == nil {
		return nil, fmt.Errorf("sshca: nil CA signer")
	}
	if len(principals) == 0 {
		return nil, fmt.Errorf("sshca: at least one principal is required")
	}
	now := time.Now()
	var serial [8]byte
	if _, err := rand.Read(serial[:]); err != nil {
		return nil, err
	}
	cert := &ssh.Certificate{
		Key:             userPub,
		Serial:          binary.BigEndian.Uint64(serial[:]),
		CertType:        ssh.UserCert,
		KeyId:           keyID,
		ValidPrincipals: principals,
		ValidAfter:      uint64(now.Add(-1 * time.Minute).Unix()), // small clock leeway
		ValidBefore:     uint64(now.Add(ttl).Unix()),
		Permissions: ssh.Permissions{
			Extensions: map[string]string{
				"permit-pty":     "",
				"permit-user-rc": "",
			},
		},
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		return nil, fmt.Errorf("sshca: sign cert: %w", err)
	}
	return cert, nil
}

// MarshalCert renders a certificate in the authorized_keys one-line form
// ("ssh-ed25519-cert-v01@openssh.com AAAA... keyid") for transport to a client,
// which writes it next to its private key as an OpenSSH certificate.
func MarshalCert(cert *ssh.Certificate) []byte {
	return ssh.MarshalAuthorizedKey(cert)
}
