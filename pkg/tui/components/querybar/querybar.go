package querybar

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/humanlogio/api/go/pkg/logql"
	typesv1 "github.com/humanlogio/api/go/types/v1"
)

type ValidatorFunc func(str string) []string

type QueryBar struct {
	textArea    textarea.Model
	submitQuery key.Binding
	validator   ValidatorFunc

	problems []string
}

type SubmitQueryMsg struct {
	Query *typesv1.LogQuery
}

func NewQueryBar(submitQuery key.Binding, validator ValidatorFunc) *QueryBar {
	ta := textarea.New()
	ta.Prompt = "┃ "
	ta.Placeholder = "Query..."
	ta.Focus()
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithDisabled())
	return &QueryBar{
		textArea:    ta,
		submitQuery: submitQuery,
		validator:   validator,
	}
}

func (m *QueryBar) Init() tea.Cmd {
	return textarea.Blink
}

func (m *QueryBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
	)
	m.textArea, tiCmd = m.textArea.Update(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.textArea.SetWidth(msg.Width)
	case tea.KeyMsg:
		if key.Matches(msg, m.submitQuery) {
			q := m.textArea.Value()
			qq, err := logql.Parse(q)
			if err != nil {
				m.problems = append(m.problems, err.Error())
			} else {
				return m, func() tea.Msg {
					return &SubmitQueryMsg{Query: qq}
				}
			}
		}
	}

	m.problems = m.validator(m.textArea.Value())

	return m, tea.Batch(tiCmd)
}

func (m *QueryBar) View() string {
	return m.textArea.View()
}
