package setup_test

import (
	"crypto/ed25519"
	"crypto/rand"
)

func generateEd25519KeyRaw() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, err
}
