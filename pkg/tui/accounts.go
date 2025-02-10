package tui

import (
	"context"
	"errors"
	"log"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	organizationv1 "github.com/humanlogio/api/go/svc/organization/v1"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
)

type environmentSelectorShell struct {
	appStyle           lipgloss.Style
	ctx                context.Context
	organizationClient organizationv1connect.OrganizationServiceClient
	state              *state.State

	children tea.Model

	table table.Model

	environments []*typesv1.Environment
	nextCursor   *typesv1.Cursor
	selected     *typesv1.Environment

	err error
}

func WithEnvironmentSelectorShell(
	appStyle lipgloss.Style,
	ctx context.Context,
	state *state.State,
	organizationClient organizationv1connect.OrganizationServiceClient,
	children tea.Model,
) *environmentSelectorShell {

	columns := []table.Column{
		{Title: "Name", Width: 10},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(5),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	return &environmentSelectorShell{
		appStyle:           appStyle,
		ctx:                ctx,
		state:              state,
		children:           children,
		organizationClient: organizationClient,
		table:              t,
	}
}

func (m *environmentSelectorShell) Init() tea.Cmd {
	log.Printf("environment: view")
	return m.children.Init()
}

func (m *environmentSelectorShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("environment: update")
	switch msg := msg.(type) {

	case *SelectedOrganizationMsg:
		log.Printf("environment: got org selected orgID=%d", msg.Organization.Id)
		return m, listEnvironmentsCmd(m.ctx, m.organizationClient, m.state)

	case listEnvironmentMsg:
		m.environments = msg.environments
		m.nextCursor = msg.next
		rows := make([]table.Row, 0, len(m.environments))
		for _, acct := range m.environments {
			rows = append(rows, table.Row{
				acct.Name,
			})
			if m.state.CurrentEnvironmentID != nil && *m.state.CurrentEnvironmentID == acct.Id {
				m.selected = acct
				break
			}
		}
		if len(m.environments) == 1 {
			m.selected = m.environments[0]
			return m, writeSelectedEnvironmentToState(m.state, m.selected)
		}
		m.table.SetRows(rows)

	case errMsg:
		m.err = msg
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.selected == nil {
				selectedName := string(m.table.SelectedRow()[0])
				for _, environment := range m.environments {
					if environment.Name == selectedName {
						m.selected = environment
						break
					}
				}
				return m, writeSelectedEnvironmentToState(m.state, m.selected)
			}
		}
	case tea.WindowSizeMsg:
		var ccmd, tcmd tea.Cmd
		m.children, ccmd = m.children.Update(msg)
		m.table, tcmd = m.table.Update(msg)
		return m, tea.Batch(ccmd, tcmd)
	}

	var cmd tea.Cmd
	if m.selected != nil {
		m.children, cmd = m.children.Update(msg)
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m *environmentSelectorShell) View() string {
	log.Printf("environment: view")
	if m.environments == nil {
		return "Looking up environments..."
	}
	if m.selected == nil {
		return m.appStyle.Render(
			"Select an environment\n",
			m.table.View(),
		)
	}
	return m.children.View()
}

type listEnvironmentMsg struct {
	environments []*typesv1.Environment
	next         *typesv1.Cursor
}

func listEnvironmentsCmd(
	ctx context.Context,
	organizationClient organizationv1connect.OrganizationServiceClient,
	state *state.State,
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("environment: listEnvironments")
		res, err := organizationClient.ListEnvironment(ctx, connect.NewRequest(&organizationv1.ListEnvironmentRequest{
			Cursor: nil,
			Limit:  10,
		}))
		if err != nil {
			cerr := new(connect.Error)
			if errors.As(err, &cerr) {
				log.Printf("environment: listEnvironments err=%v", cerr)
			}
			return errMsg{err}
		}
		out := listEnvironmentMsg{
			environments: make([]*typesv1.Environment, 0, len(res.Msg.Items)),
		}
		for _, mc := range res.Msg.Items {
			out.environments = append(out.environments, mc.Environment)
		}
		log.Printf("environment: got %d environments", len(out.environments))
		out.next = res.Msg.Next
		return out
	}
}

type SelectedEnvironmentMsg struct {
	Environment *typesv1.Environment
}

func writeSelectedEnvironmentToState(state *state.State, selected *typesv1.Environment) func() tea.Msg {
	return func() tea.Msg {
		log.Print("app: writeSelectedEnvironmentToState")
		state.CurrentEnvironmentID = &selected.Id
		if err := state.WriteBack(); err != nil {
			return errMsg{err}
		}
		return &SelectedEnvironmentMsg{Environment: selected}
	}
}
