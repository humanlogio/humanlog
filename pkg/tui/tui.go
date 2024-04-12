package tui

import (
	"context"
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/humanlogio/api/go/svc/account/v1/accountv1connect"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	"github.com/humanlogio/humanlog/internal/pkg/state"
)

func RunTUI(
	ctx context.Context,
	state *state.State,
	userClient userv1connect.UserServiceClient,
	organizationClient organizationv1connect.OrganizationServiceClient,
	accountClient accountv1connect.AccountServiceClient,
	queryClient queryv1connect.QueryServiceClient,
) error {
	appStyle := lipgloss.NewStyle().Padding(1, 2)

	var p *tea.Program
	app := &model{
		appStyle: appStyle,
		state:    state,
		orgSelectorShell: WithOrgSelectorShell(appStyle, ctx, state, userClient,
			WithAccountSelectorShell(appStyle, ctx, state, organizationClient,
				// WithMachineSelectorShell(appStyle, ctx, state, accountClient,
				NewQuerierModel(appStyle, ctx, state, queryClient, func(m tea.Msg) {
					p.Send(m)
				}),
				// ),
			),
		),
	}
	p = tea.NewProgram(app)

	f, err := tea.LogToFile("debug.log", "humanlog")
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := p.Run(); err != nil {
		log.Printf("program errored: %v", err)
		return err
	}
	log.Printf("program exited")
	return nil
}

type model struct {
	appStyle lipgloss.Style

	state *state.State

	err errMsg

	orgSelectorShell tea.Model
}

func (m *model) Init() tea.Cmd {
	log.Printf("app: init")
	return m.orgSelectorShell.Init()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("app: update: %T -> %#v", msg, msg)
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg
		log.Printf("err=%v", msg.Error())
		return m, tea.Quit
	case tea.KeyMsg:
		log.Printf("app: key-press: %q", msg.String())
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.orgSelectorShell, cmd = m.orgSelectorShell.Update(msg)
	return m, cmd
}

func (m *model) View() string {
	log.Printf("app: view")
	return m.appStyle.Render(m.orgSelectorShell.View())
}

type errMsg struct{ err error }

// For messages that contain errors it's often handy to also implement the
// error interface on the message.
func (e errMsg) Error() string { return e.err.Error() }
