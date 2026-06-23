package sshca

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func mustSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSignUserCert(t *testing.T) {
	ca := mustSigner(t)
	user := mustSigner(t)

	cert, err := SignUserCert(ca, user.PublicKey(), "alice@laptop", []string{"alice"}, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if cert.CertType != ssh.UserCert {
		t.Fatalf("cert type = %d, want UserCert", cert.CertType)
	}
	if len(cert.ValidPrincipals) != 1 || cert.ValidPrincipals[0] != "alice" {
		t.Fatalf("principals = %v", cert.ValidPrincipals)
	}
	if _, ok := cert.Permissions.Extensions["permit-pty"]; !ok {
		t.Fatal("missing permit-pty extension")
	}

	// the certificate must verify against the CA, for principal alice, now.
	checker := &ssh.CertChecker{
		IsUserAuthority: func(k ssh.PublicKey) bool { return string(k.Marshal()) == string(ca.PublicKey().Marshal()) },
	}
	if err := checker.CheckCert("alice", cert); err != nil {
		t.Fatalf("CheckCert(alice): %v", err)
	}
	if err := checker.CheckCert("bob", cert); err == nil {
		t.Fatal("CheckCert(bob) should fail — bob is not a principal")
	}
}

func TestSignUserCertExpired(t *testing.T) {
	ca := mustSigner(t)
	user := mustSigner(t)
	cert, err := SignUserCert(ca, user.PublicKey(), "id", []string{"alice"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert.ValidBefore = uint64(time.Now().Add(-time.Minute).Unix()) // force-expire (re-sign not needed for CheckCert clock)
	checker := &ssh.CertChecker{
		IsUserAuthority: func(k ssh.PublicKey) bool { return true },
	}
	if err := checker.CheckCert("alice", cert); err == nil {
		t.Fatal("expired cert should fail CheckCert")
	}
}
