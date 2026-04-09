package users

import (
	"fmt"
	"regexp"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
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

// RunRegistration presents a TUI form over the SSH session to collect a username.
func RunRegistration(store *Store, sess ssh.Session) (string, error) {
	var username string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Welcome to Hopbox!").
				Description("Choose a username for your dev environment.").
				Placeholder("username").
				Value(&username).
				Validate(func(s string) error {
					if err := ValidateUsername(s); err != nil {
						return err
					}
					if store.IsUsernameTaken(s) {
						return fmt.Errorf("username %q is already taken", s)
					}
					return nil
				}),
		),
	).WithProgramOptions(bubbletea.MakeOptions(sess)...)

	if err := form.Run(); err != nil {
		return "", fmt.Errorf("registration form: %w", err)
	}

	return username, nil
}
