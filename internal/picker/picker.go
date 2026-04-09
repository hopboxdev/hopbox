package picker

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish/bubbletea"
)

type pickerModel struct {
	form     *huh.Form
	selected *string
	aborted  bool
}

func (m pickerModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateAborted {
		m.aborted = true
		return m, tea.Quit
	}

	if m.form.State == huh.StateCompleted {
		return m, tea.Quit
	}

	return m, cmd
}

func (m pickerModel) View() string {
	return m.form.View()
}

// RunPicker shows a box selection TUI and returns the chosen box name.
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

	m := pickerModel{form: form, selected: &selected}
	p := tea.NewProgram(m, bubbletea.MakeOptions(sess)...)

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

	pm := result.(pickerModel)
	if pm.aborted {
		return "", fmt.Errorf("picker cancelled")
	}

	return selected, nil
}
