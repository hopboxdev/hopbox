package users

import (
	"fmt"
	"regexp"

	tea "github.com/charmbracelet/bubbletea"
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
	pty, _, ok := sess.Pty()
	if !ok {
		return "", fmt.Errorf("no PTY available")
	}

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
	)

	opts := bubbletea.MakeOptions(sess)
	p := tea.NewProgram(form, opts...)

	// Send initial window size only — no resize goroutine needed for a simple input
	go func() {
		p.Send(tea.WindowSizeMsg{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		})
	}()

	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("registration form: %w", err)
	}

	f := result.(*huh.Form)
	if f.State == huh.StateAborted {
		return "", fmt.Errorf("registration cancelled")
	}

	return username, nil
}
