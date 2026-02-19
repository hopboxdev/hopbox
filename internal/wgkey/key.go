package wgkey

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// KeyPair holds a WireGuard private/public key pair.
type KeyPair struct {
	private wgtypes.Key
	public  wgtypes.Key
}

// Generate creates a new random WireGuard key pair.
func Generate() (*KeyPair, error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}
	return &KeyPair{
		private: priv,
		public:  priv.PublicKey(),
	}, nil
}

// PrivateKeyHex returns the private key as a 64-character hex string.
func (kp *KeyPair) PrivateKeyHex() string {
	b := kp.private[:]
	return hex.EncodeToString(b)
}

// PublicKeyHex returns the public key as a 64-character hex string.
func (kp *KeyPair) PublicKeyHex() string {
	b := kp.public[:]
	return hex.EncodeToString(b)
}

// PrivateKeyBase64 returns the private key base64-encoded (for config storage).
func (kp *KeyPair) PrivateKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.private[:])
}

// PublicKeyBase64 returns the public key base64-encoded (for config storage).
func (kp *KeyPair) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(kp.public[:])
}

// SaveToFile writes the key pair (base64) to a file. The directory is created
// if it does not exist.
func (kp *KeyPair) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create key dir: %w", err)
	}
	content := fmt.Sprintf("private=%s\npublic=%s\n",
		kp.PrivateKeyBase64(), kp.PublicKeyBase64())
	return os.WriteFile(path, []byte(content), 0600)
}

// LoadFromFile reads a key pair from a file written by SaveToFile.
func LoadFromFile(path string) (*KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	var privB64, pubB64 string
	for _, line := range splitLines(string(data)) {
		if k, v, ok := cutLine(line); ok {
			switch k {
			case "private":
				privB64 = v
			case "public":
				pubB64 = v
			}
		}
	}
	if privB64 == "" || pubB64 == "" {
		return nil, fmt.Errorf("invalid key file: missing private or public key")
	}

	privBytes, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}

	var kp KeyPair
	if len(privBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid private key length: %d", len(privBytes))
	}
	if len(pubBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid public key length: %d", len(pubBytes))
	}
	copy(kp.private[:], privBytes)
	copy(kp.public[:], pubBytes)
	return &kp, nil
}

// FromHex parses a key pair from hex-encoded private and public keys.
func FromHex(privHex, pubHex string) (*KeyPair, error) {
	privBytes, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, fmt.Errorf("decode private key hex: %w", err)
	}
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil {
		return nil, fmt.Errorf("decode public key hex: %w", err)
	}
	if len(privBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid private key length")
	}
	if len(pubBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid public key length")
	}
	var kp KeyPair
	copy(kp.private[:], privBytes)
	copy(kp.public[:], pubBytes)
	return &kp, nil
}

// FromBase64 parses a key pair from base64-encoded private and public keys.
func FromBase64(privB64, pubB64 string) (*KeyPair, error) {
	privBytes, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(privBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid private key length")
	}
	if len(pubBytes) != wgtypes.KeyLen {
		return nil, fmt.Errorf("invalid public key length")
	}
	var kp KeyPair
	copy(kp.private[:], privBytes)
	copy(kp.public[:], pubBytes)
	return &kp, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// KeyB64ToHex decodes a base64-encoded WireGuard key and returns it as a
// 64-character hex string suitable for WireGuard IPC.
func KeyB64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("decode base64 key: %w", err)
	}
	if len(raw) != wgtypes.KeyLen {
		return "", fmt.Errorf("invalid key length: %d", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func cutLine(s string) (key, value string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
