//go:build exclude

package main

import (
	"log"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/humanlogio/humanlog/pkg/tui/components/modal"
)

func main() {

	style := lipgloss.NewStyle()
	modes := []*modal.Mode{
		{
			EnterModeKey: key.NewBinding(
				key.WithKeys("q"),
			),
			Name: "query",
			Component: func() tea.Model {
				ti := textinput.New()
				ti.Prompt = "query"
				return textInput{ti}
			}(),
		},
		{
			EnterModeKey: key.NewBinding(
				key.WithKeys("n"),
			),
			Name: "navigate",
			Component: func() tea.Model {
				ti := textinput.New()
				ti.Prompt = "fzf..."
				return textInput{ti}
			}(),
		},
		{
			EnterModeKey: key.NewBinding(
				key.WithKeys("a"),
			),
			Name: "auth",
			Component: func() tea.Model {
				ti := textinput.New()
				ti.Prompt = "fzf..."
				return textInput{ti}
			}(),
		},
	}

	exitKey := key.NewBinding(key.WithKeys("esc"))

	if _, err := tea.NewProgram(modal.NewModal(style, modes, exitKey)).Run(); err != nil {
		log.Fatal(err)
	}
}

type textInput struct {
	m textinput.Model
}

func (m textInput) Init() tea.Cmd {
	return nil
}

func (m textInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.m, cmd = m.m.Update(msg)
	return m, cmd
}

func (m textInput) View() string {
	return m.m.View()
}
