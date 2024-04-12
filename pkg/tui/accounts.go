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

type accountSelectorShell struct {
	appStyle           lipgloss.Style
	ctx                context.Context
	organizationClient organizationv1connect.OrganizationServiceClient
	state              *state.State

	children tea.Model

	table table.Model

	accounts   []*typesv1.Account
	nextCursor *typesv1.Cursor
	selected   *typesv1.Account

	err error
}

func WithAccountSelectorShell(
	appStyle lipgloss.Style,
	ctx context.Context,
	state *state.State,
	organizationClient organizationv1connect.OrganizationServiceClient,
	children tea.Model,
) *accountSelectorShell {

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
	return &accountSelectorShell{
		appStyle:           appStyle,
		ctx:                ctx,
		state:              state,
		children:           children,
		organizationClient: organizationClient,
		table:              t,
	}
}

func (m *accountSelectorShell) Init() tea.Cmd {
	log.Printf("account: view")
	return m.children.Init()
}

func (m *accountSelectorShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("account: update")
	switch msg := msg.(type) {

	case *SelectedOrganizationMsg:
		log.Printf("account: got org selected orgID=%d", msg.Organization.Id)
		return m, listAccountsCmd(m.ctx, m.organizationClient, m.state)

	case listAccountMsg:
		m.accounts = msg.accounts
		m.nextCursor = msg.next
		rows := make([]table.Row, 0, len(m.accounts))
		for _, acct := range m.accounts {
			rows = append(rows, table.Row{
				acct.Name,
			})
			if m.state.CurrentAccountID != nil && *m.state.CurrentAccountID == acct.Id {
				m.selected = acct
				break
			}
		}
		if len(m.accounts) == 1 {
			m.selected = m.accounts[0]
			return m, writeSelectedAccountToState(m.state, m.selected)
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
				for _, account := range m.accounts {
					if account.Name == selectedName {
						m.selected = account
						break
					}
				}
				return m, writeSelectedAccountToState(m.state, m.selected)
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

func (m *accountSelectorShell) View() string {
	log.Printf("account: view")
	if m.accounts == nil {
		return "Looking up accounts..."
	}
	if m.selected == nil {
		return m.appStyle.Render(
			"Select an account\n",
			m.table.View(),
		)
	}
	return m.children.View()
}

type listAccountMsg struct {
	accounts []*typesv1.Account
	next     *typesv1.Cursor
}

func listAccountsCmd(
	ctx context.Context,
	organizationClient organizationv1connect.OrganizationServiceClient,
	state *state.State,
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("account: listAccounts")
		res, err := organizationClient.ListAccount(ctx, connect.NewRequest(&organizationv1.ListAccountRequest{
			Cursor:         nil,
			Limit:          10,
			OrganizationId: *state.CurrentOrgID,
		}))
		if err != nil {
			cerr := new(connect.Error)
			if errors.As(err, &cerr) {
				log.Printf("account: listAccounts err=%v", cerr)
			}
			return errMsg{err}
		}
		out := listAccountMsg{
			accounts: make([]*typesv1.Account, 0, len(res.Msg.Items)),
		}
		for _, mc := range res.Msg.Items {
			out.accounts = append(out.accounts, mc.Account)
		}
		log.Printf("account: got %d accounts", len(out.accounts))
		out.next = res.Msg.Next
		return out
	}
}

type SelectedAccountMsg struct {
	Account *typesv1.Account
}

func writeSelectedAccountToState(state *state.State, selected *typesv1.Account) func() tea.Msg {
	return func() tea.Msg {
		log.Print("app: writeSelectedAccountToState")
		state.CurrentAccountID = &selected.Id
		if err := state.WriteBack(); err != nil {
			return errMsg{err}
		}
		return &SelectedAccountMsg{Account: selected}
	}
}
