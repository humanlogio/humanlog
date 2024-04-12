//go:build exclude

package main

import (
	"log"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/humanlogio/humanlog/pkg/tui/components/keyhandler"
	"github.com/humanlogio/humanlog/pkg/tui/components/modal"
)

func main() {

	baseStyle := lipgloss.NewStyle().
		PaddingLeft(1).
		PaddingRight(1).
		Foreground(lipgloss.Color("#282828"))

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

	app := keyhandler.Handle(
		key.NewBinding(key.WithKeys("ctrl+d")),
		tea.Batch(
			tea.ClearScreen,
			tea.Quit,
		),
		modal.NewModal(baseStyle, modes, exitKey),
	)

	if _, err := tea.NewProgram(app).Run(); err != nil {
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
