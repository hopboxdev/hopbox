package setup_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/setup"
)

func TestMarshalHostKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey()

	marshalled := setup.MarshalHostKey(pub)
	if marshalled == "" {
		t.Fatal("MarshalHostKey returned empty string")
	}

	// Must be parseable back
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(marshalled))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	if pub.Type() != parsed.Type() {
		t.Errorf("type = %q, want %q", parsed.Type(), pub.Type())
	}
	if base64.StdEncoding.EncodeToString(pub.Marshal()) !=
		base64.StdEncoding.EncodeToString(parsed.Marshal()) {
		t.Error("marshalled key does not round-trip")
	}
}

func TestMarshalHostKeyNil(t *testing.T) {
	if got := setup.MarshalHostKey(nil); got != "" {
		t.Errorf("MarshalHostKey(nil) = %q, want empty", got)
	}
}

func TestHostKeyCallbackFor_Empty(t *testing.T) {
	_, err := setup.HostKeyCallbackFor("")
	if err == nil {
		t.Error("expected error for empty saved key")
	}
}

func TestHostKeyCallbackFor_Invalid(t *testing.T) {
	_, err := setup.HostKeyCallbackFor("not a valid key")
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestHostKeyCallbackFor_Enforces(t *testing.T) {
	// Generate two different keys.
	_, privA, _ := ed25519.GenerateKey(rand.Reader)
	_, privB, _ := ed25519.GenerateKey(rand.Reader)
	signerA, _ := ssh.NewSignerFromKey(privA)
	signerB, _ := ssh.NewSignerFromKey(privB)

	// Save key A, then verify that key B is rejected.
	savedA := setup.MarshalHostKey(signerA.PublicKey())
	cb, err := setup.HostKeyCallbackFor(savedA)
	if err != nil {
		t.Fatalf("HostKeyCallbackFor: %v", err)
	}

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

	// Correct key: accepted.
	if err := cb("127.0.0.1:22", addr, signerA.PublicKey()); err != nil {
		t.Errorf("callback rejected correct key: %v", err)
	}

	// Wrong key: rejected.
	if err := cb("127.0.0.1:22", addr, signerB.PublicKey()); err == nil {
		t.Error("callback should have rejected wrong key")
	}
}

func TestServerPubKeyFromB64(t *testing.T) {
	// 32 zero bytes as base64
	raw := make([]byte, 32)
	b64 := base64.StdEncoding.EncodeToString(raw)

	hex, err := setup.ServerPubKeyFromB64(b64)
	if err != nil {
		t.Fatalf("ServerPubKeyFromB64: %v", err)
	}
	if len(hex) != 64 {
		t.Errorf("hex length = %d, want 64", len(hex))
	}
	for _, c := range hex {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in output", c)
		}
	}
}

func TestServerPubKeyFromB64_WrongLength(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("tooshort"))
	_, err := setup.ServerPubKeyFromB64(b64)
	if err == nil {
		t.Error("expected error for wrong-length key")
	}
}

func TestServerPubKeyFromB64_InvalidBase64(t *testing.T) {
	_, err := setup.ServerPubKeyFromB64("not!base64!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestLoadSigners_NoKeys(t *testing.T) {
	// Point HOME at an empty temp dir so no key files exist,
	// and clear SSH_AUTH_SOCK so the SSH agent is not consulted.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := setup.LoadSigners("")
	if err == nil {
		t.Error("expected error when no SSH keys exist")
	}
}
