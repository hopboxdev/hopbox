package wgkey_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/wgkey"
)

func TestGenerate(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	privHex := kp.PrivateKeyHex()
	pubHex := kp.PublicKeyHex()

	if len(privHex) != 64 {
		t.Errorf("private key hex length = %d, want 64", len(privHex))
	}
	if len(pubHex) != 64 {
		t.Errorf("public key hex length = %d, want 64", len(pubHex))
	}
}

func TestGenerateUnique(t *testing.T) {
	kp1, _ := wgkey.Generate()
	kp2, _ := wgkey.Generate()
	if kp1.PrivateKeyHex() == kp2.PrivateKeyHex() {
		t.Error("two generated keys are identical")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	if err := kp.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	loaded, err := wgkey.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if kp.PrivateKeyHex() != loaded.PrivateKeyHex() {
		t.Error("private key mismatch after round-trip")
	}
	if kp.PublicKeyHex() != loaded.PublicKeyHex() {
		t.Error("public key mismatch after round-trip")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	kp, _ := wgkey.Generate()
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "test.key")

	if err := kp.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile with nested dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestFilePermissions(t *testing.T) {
	kp, _ := wgkey.Generate()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	_ = kp.SaveToFile(path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestFromHexRoundTrip(t *testing.T) {
	kp, _ := wgkey.Generate()
	loaded, err := wgkey.FromHex(kp.PrivateKeyHex(), kp.PublicKeyHex())
	if err != nil {
		t.Fatalf("FromHex: %v", err)
	}
	if kp.PrivateKeyHex() != loaded.PrivateKeyHex() {
		t.Error("private key mismatch")
	}
}

func TestFromBase64RoundTrip(t *testing.T) {
	kp, _ := wgkey.Generate()
	loaded, err := wgkey.FromBase64(kp.PrivateKeyBase64(), kp.PublicKeyBase64())
	if err != nil {
		t.Fatalf("FromBase64: %v", err)
	}
	if kp.PrivateKeyHex() != loaded.PrivateKeyHex() {
		t.Error("private key mismatch")
	}
}
