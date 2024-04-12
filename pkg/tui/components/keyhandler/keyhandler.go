package keyhandler

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type Shell struct {
	child tea.Model
	key   key.Binding
	cmd   tea.Cmd
}

type State int

var _ tea.Model = (*Shell)(nil)

func Handle(key key.Binding, cmd tea.Cmd, child tea.Model) *Shell {
	return &Shell{
		child: child,
		key:   key,
		cmd:   cmd,
	}
}

func (mdl *Shell) Init() tea.Cmd {
	return mdl.child.Init()
}

func (mdl *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(msg, mdl.key) {
			return mdl, mdl.cmd
		}
	}
	var cmd tea.Cmd
	mdl.child, cmd = mdl.child.Update(msg)
	return mdl, cmd
}

func (mdl *Shell) View() string {
	return mdl.child.View()
}
