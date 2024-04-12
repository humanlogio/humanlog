package modal

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
)

const (
	SelectingMode State = iota
	UsingMode
)

type Mode struct {
	EnterModeKey key.Binding
	Name         string
	Component    tea.Model
}

type Modal struct {
	modeStyle lipgloss.Style
	current   *Mode
	modes     []*Mode

	exitModeKey key.Binding
}

type State int

var _ tea.Model = (*Modal)(nil)

func NewModal(baseStyle lipgloss.Style, modes []*Mode, exitModeKey key.Binding) *Modal {
	modeStyle := baseStyle.Copy().
		Align(lipgloss.Center).
		Bold(true).
		Background(lipgloss.Color("#b8bb26"))

	return &Modal{
		modeStyle:   modeStyle,
		modes:       modes,
		exitModeKey: exitModeKey,
	}
}

func (mdl *Modal) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, mode := range mdl.modes {
		cmds = append(cmds, mode.Component.Init())
	}
	return tea.Batch(cmds...)
}

func (mdl *Modal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	if mdl.current == nil {
		// we're selecting a mode
		if msg, ok := msg.(tea.KeyMsg); ok {
			for _, mode := range mdl.modes {
				if key.Matches(msg, mode.EnterModeKey) {
					mdl.current = mode
					var cmd tea.Cmd
					mdl.current.Component, cmd = mdl.current.Component.Update(msg)
					return mdl, cmd
				}
			}
		}
		return mdl, nil
	}
	// we're inside a mode
	if msg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(msg, mdl.exitModeKey) {
			mdl.current = nil
			return mdl, nil
		}
	}
	var cmd tea.Cmd
	mdl.current.Component, cmd = mdl.current.Component.Update(msg)
	return mdl, cmd
}

func (mdl *Modal) View() string {
	if mdl.current == nil {

		rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("99")).MarginRight(1)
		enumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).MarginRight(1)

		t := tree.New().Root(
			mdl.modeStyle.Render("mode"),
		).ItemStyle(rootStyle).EnumeratorStyle(enumStyle)
		for _, mode := range mdl.modes {
			t.Child(
				"(" + mode.EnterModeKey.Keys()[0] + ") ─> " + mode.Name,
			)
		}
		return t.String()
	}

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#282828")).
		Background(lipgloss.Color("#7c6f64"))

	modeFooter := mdl.modeStyle.Render(mdl.current.Name)

	footer := footerStyle.Render(modeFooter, " <- "+mdl.current.EnterModeKey.Keys()[0])

	return lipgloss.JoinVertical(lipgloss.Left,
		mdl.current.Component.View(),
		footer,
	)
}
