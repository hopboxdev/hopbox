package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
)

// StatusCmd shows tunnel and workspace health.
type StatusCmd struct{}

func (c *StatusCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	p := tea.NewProgram(newDashModel(hostName, cfg))
	_, err = p.Run()
	return err
}

// dashModel is the Bubble Tea model for the status dashboard.
type dashModel struct {
	hostName string
	cfg      *hostconfig.HostConfig
	data     dashData
	width    int
	quitting bool
}

func newDashModel(hostName string, cfg *hostconfig.HostConfig) dashModel {
	return dashModel{
		hostName: hostName,
		cfg:      cfg,
		data:     fetchDashData(hostName, cfg),
		width:    80,
	}
}

// Messages.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type refreshMsg struct {
	data dashData
}

func (m dashModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		return refreshMsg{data: fetchDashData(m.hostName, m.cfg)}
	}
}

func (m dashModel) Init() tea.Cmd {
	return tickCmd()
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.fetchCmd()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.fetchCmd(), tickCmd())

	case refreshMsg:
		m.data = msg.data
		return m, nil
	}

	return m, nil
}

func (m dashModel) View() string {
	if m.quitting {
		return ""
	}
	return renderDashboard(m.data, m.width)
}
