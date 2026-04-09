package control

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateCode_Format(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	parts := strings.Split(code, "-")
	if len(parts) != 2 {
		t.Fatalf("expected XXXX-XXXX format, got %q", code)
	}
	if len(parts[0]) != 4 || len(parts[1]) != 4 {
		t.Fatalf("expected 4-4 character parts, got %q", code)
	}

	// All characters should be uppercase alphanumeric from codeCharset
	for _, c := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(codeCharset, c) {
			t.Fatalf("unexpected character %q in code %q", string(c), code)
		}
	}
}

func TestValidateCode_Success(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	fp, err := s.ValidateCode(code)
	if err != nil {
		t.Fatalf("ValidateCode: %v", err)
	}
	if fp != "SHA256_abc123" {
		t.Fatalf("expected fingerprint SHA256_abc123, got %q", fp)
	}
}

func TestValidateCode_ConsumedOnUse(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	_, err = s.ValidateCode(code)
	if err != nil {
		t.Fatalf("first validate: %v", err)
	}

	_, err = s.ValidateCode(code)
	if err == nil {
		t.Fatal("expected error on second validate (code consumed)")
	}
}

func TestValidateCode_InvalidCode(t *testing.T) {
	s := NewLinkStore()
	_, err := s.ValidateCode("NOPE-NOPE")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestValidateCode_Expired(t *testing.T) {
	s := NewLinkStore()
	code, err := s.GenerateCode("SHA256_abc123")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	// Manually expire the code
	s.mu.Lock()
	lc := s.codes[code]
	lc.ExpiresAt = time.Now().Add(-1 * time.Minute)
	s.codes[code] = lc
	s.mu.Unlock()

	_, err = s.ValidateCode(code)
	if err == nil {
		t.Fatal("expected error for expired code")
	}
}
