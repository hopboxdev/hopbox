package picker

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
)

// RunPicker shows a box selection TUI and returns the chosen box name.
// Uses the same single-tea.Program pattern as the wizard.
func RunPicker(boxes []string, sess ssh.Session) (string, error) {
	if len(boxes) == 0 {
		return "", fmt.Errorf("no boxes found")
	}

	if len(boxes) == 1 {
		return boxes[0], nil
	}

	pty, winCh, ok := sess.Pty()
	if !ok {
		return "", fmt.Errorf("no PTY available")
	}

	var selected string
	options := make([]huh.Option[string], len(boxes))
	for i, box := range boxes {
		options[i] = huh.NewOption(box, box)
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select a box").
			Options(options...).
			Value(&selected),
	))

	p := tea.NewProgram(form, bubbletea.MakeOptions(sess)...)

	go func() {
		p.Send(tea.WindowSizeMsg{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		})
	}()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case w, ok := <-winCh:
				if !ok {
					return
				}
				p.Send(tea.WindowSizeMsg{
					Width:  w.Width,
					Height: w.Height,
				})
			}
		}
	}()

	result, err := p.Run()
	close(done)
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}

	f := result.(*huh.Form)
	if f.State == huh.StateAborted {
		return "", fmt.Errorf("picker cancelled")
	}

	return selected, nil
}
