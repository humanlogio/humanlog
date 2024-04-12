package tui

import (
	"context"
	"errors"
	"log"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
)

type orgSelectorShell struct {
	appStyle   lipgloss.Style
	ctx        context.Context
	userClient userv1connect.UserServiceClient
	state      *state.State

	children tea.Model

	table table.Model

	organizations []*typesv1.Organization
	nextCursor    *typesv1.Cursor
	selected      *typesv1.Organization

	err error
}

func WithOrgSelectorShell(
	appStyle lipgloss.Style,
	ctx context.Context,
	state *state.State,
	userClient userv1connect.UserServiceClient,
	children tea.Model,
) *orgSelectorShell {

	columns := []table.Column{
		{Title: "Name", Width: 10},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(3),
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
	return &orgSelectorShell{
		appStyle:   appStyle,
		ctx:        ctx,
		state:      state,
		children:   children,
		userClient: userClient,
		table:      t,
	}
}

func (m *orgSelectorShell) Init() tea.Cmd {
	log.Printf("org: init")
	return tea.Batch(
		listOrganizationsCmd(m.ctx, m.userClient, m.state),
		m.children.Init(),
	)
}

func (m *orgSelectorShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("org: update")
	switch msg := msg.(type) {

	case listOrganizationMsg:
		m.organizations = msg.organizations
		m.nextCursor = msg.next
		rows := make([]table.Row, 0, len(m.organizations))
		for _, org := range m.organizations {
			rows = append(rows, table.Row{
				org.Name,
			})
			if m.state.CurrentOrgID != nil && *m.state.CurrentOrgID == org.Id {
				m.selected = org
				break
			}
		}
		if len(m.organizations) == 1 {
			m.selected = m.organizations[0]
			return m, writeSelectedOrgToState(m.state, m.selected)
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
				selectName := string(m.table.SelectedRow()[0])
				for _, item := range m.organizations {
					if item.Name == selectName {
						m.selected = item
						break
					}
				}
				return m, writeSelectedOrgToState(m.state, m.selected)
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

func (m *orgSelectorShell) View() string {
	log.Printf("org: view")
	if m.organizations == nil {
		return "Looking up organizations..."
	}
	if m.selected == nil {
		return m.appStyle.Render(
			"Select an organization\n",
			m.table.View(),
		)
	}
	return m.children.View()
}

type listOrganizationMsg struct {
	organizations []*typesv1.Organization
	next          *typesv1.Cursor
}

func listOrganizationsCmd(
	ctx context.Context,
	userClient userv1connect.UserServiceClient,
	state *state.State,
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("organization: listOrgs")
		res, err := userClient.ListOrganization(ctx, connect.NewRequest(&userv1.ListOrganizationRequest{
			Cursor: nil,
			Limit:  10,
		}))
		if err != nil {
			cerr := new(connect.Error)
			if errors.As(err, &cerr) {
				log.Printf("organization: listOrgs err=%v", cerr)
			}
			return errMsg{err}
		}
		out := listOrganizationMsg{
			organizations: make([]*typesv1.Organization, 0, len(res.Msg.Items)),
		}
		for _, mc := range res.Msg.Items {
			log.Printf("organization: got org %q", mc.Organization.Name)
			out.organizations = append(out.organizations, mc.Organization)
		}
		log.Printf("organization: got %d orgs", len(out.organizations))
		out.next = res.Msg.Next
		return out
	}
}

type SelectedOrganizationMsg struct {
	Organization *typesv1.Organization
}

func writeSelectedOrgToState(state *state.State, selected *typesv1.Organization) func() tea.Msg {
	return func() tea.Msg {
		log.Print("app: writeSelectedOrgToState")
		state.CurrentOrgID = &selected.Id
		if err := state.WriteBack(); err != nil {
			return errMsg{err}
		}
		return &SelectedOrganizationMsg{Organization: selected}
	}
}
