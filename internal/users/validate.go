package users

import (
	"fmt"
	"regexp"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateUsername checks that a username is lowercase alphanumeric with hyphens,
// not starting/ending with hyphen, no consecutive hyphens.
func ValidateUsername(name string) error {
	if name == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("username must be lowercase alphanumeric with single hyphens, not starting or ending with a hyphen")
	}
	if containsDoubleHyphen(name) {
		return fmt.Errorf("username cannot contain consecutive hyphens")
	}
	return nil
}

func containsDoubleHyphen(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '-' && s[i+1] == '-' {
			return true
		}
	}
	return false
}
