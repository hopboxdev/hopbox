package control

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

const (
	codeTTL     = 5 * time.Minute
	codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I to avoid confusion
)

// LinkCode associates a one-time code with a user's fingerprint.
type LinkCode struct {
	Fingerprint string
	ExpiresAt   time.Time
}

// LinkStore holds active link codes in memory.
type LinkStore struct {
	mu    sync.Mutex
	codes map[string]LinkCode // code -> LinkCode
}

// NewLinkStore creates an empty link store.
func NewLinkStore() *LinkStore {
	return &LinkStore{
		codes: make(map[string]LinkCode),
	}
}

// GenerateCode creates a new link code for the given fingerprint.
// Returns a code in XXXX-XXXX format.
func (s *LinkStore) GenerateCode(fingerprint string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code, err := randomCode()
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}

	s.codes[code] = LinkCode{
		Fingerprint: fingerprint,
		ExpiresAt:   time.Now().Add(codeTTL),
	}

	return code, nil
}

// ValidateCode checks that a code exists and has not expired, then consumes it.
// Returns the fingerprint associated with the code.
func (s *LinkStore) ValidateCode(code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lc, ok := s.codes[code]
	if !ok {
		return "", fmt.Errorf("invalid or expired link code")
	}

	if time.Now().After(lc.ExpiresAt) {
		delete(s.codes, code)
		return "", fmt.Errorf("link code expired")
	}

	// Consume the code (one-time use)
	delete(s.codes, code)
	return lc.Fingerprint, nil
}

// randomCode generates a code in XXXX-XXXX format.
func randomCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	chars := make([]byte, 8)
	for i := range chars {
		chars[i] = codeCharset[int(b[i])%len(codeCharset)]
	}
	return fmt.Sprintf("%s-%s", string(chars[:4]), string(chars[4:])), nil
}
