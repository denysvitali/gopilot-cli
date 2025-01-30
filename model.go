package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/denysvitali/gopilot-cli/pkg"
	"strings"
)

type model struct {
	spinner       spinner.Model
	deviceCode    *copilot.DeviceCodeResponse
	c             *copilot.Copilot
	authenticated bool
	err           error
	quitting      bool
}

type deviceCodeMsg struct {
	response *copilot.DeviceCodeResponse
}

type authStatusMsg struct {
	authenticated bool
}

type errMsg struct {
	err error
}

func initialModel() *model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return &model{
		spinner: s,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			codeResp, err := m.c.GetDeviceCodeCmd()
			if err != nil {
				return errMsg{err}
			}
			return deviceCodeMsg{codeResp}
		},
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	checkAuthWrapper := func() tea.Cmd {
		return func() tea.Msg {
			done, err := m.c.CheckAuthStatusCmd(m.deviceCode.DeviceCode)
			if !done {
				return deviceCodeMsg{m.deviceCode}
			}
			if err != nil {
				return errMsg{err}
			}
			if done {
				return authStatusMsg{true}
			}
			return nil
		}
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case deviceCodeMsg:
		m.deviceCode = msg.response
		return m, checkAuthWrapper()

	case authStatusMsg:
		if msg.authenticated {
			m.authenticated = true
			return m, tea.Quit
		}
		return m, checkAuthWrapper()

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

func (m *model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %s\n", m.err)
	}

	if m.quitting {
		return "Goodbye!\n"
	}

	if m.authenticated {
		return "Authentication successful!\n"
	}

	var s strings.Builder
	s.WriteString("\n🔑 GitHub Copilot Authentication\n\n")

	if m.deviceCode != nil {
		s.WriteString(fmt.Sprintf("Please visit: %s\n", m.deviceCode.VerificationUri))
		s.WriteString(fmt.Sprintf("Enter code: %s\n\n", m.deviceCode.UserCode))
		s.WriteString(fmt.Sprintf("%s Waiting for authentication...\n", m.spinner.View()))
	} else {
		s.WriteString(fmt.Sprintf("%s Requesting device code...\n", m.spinner.View()))
	}

	return s.String()
}

func setup(c *copilot.Copilot) error {
	myModel := initialModel()
	myModel.c = c
	p := tea.NewProgram(myModel)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}
	if !myModel.authenticated {
		return fmt.Errorf("authentication failed or was cancelled")
	}
	return nil
}
