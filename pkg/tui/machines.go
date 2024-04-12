package tui

import (
	"context"
	"errors"
	"log"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	accountv1 "github.com/humanlogio/api/go/svc/account/v1"
	"github.com/humanlogio/api/go/svc/account/v1/accountv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
)

type machineSelectorShell struct {
	appStyle      lipgloss.Style
	ctx           context.Context
	state         *state.State
	accountClient accountv1connect.AccountServiceClient

	children tea.Model

	table table.Model

	nextCursor *typesv1.Cursor
	machines   []*typesv1.Machine
	selected   *typesv1.Machine

	err error
}

func WithMachineSelectorShell(
	appStyle lipgloss.Style,
	ctx context.Context,
	state *state.State,
	accountClient accountv1connect.AccountServiceClient,
	children tea.Model,
) *machineSelectorShell {

	columns := []table.Column{
		{Title: "Name", Width: 10},
		{Title: "OS", Width: 4},
		{Title: "Arch", Width: 4},
		{Title: "Hostname", Width: 10},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(7),
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
	return &machineSelectorShell{
		appStyle:      appStyle,
		ctx:           ctx,
		state:         state,
		children:      children,
		accountClient: accountClient,
		table:         t,
	}
}

func (m *machineSelectorShell) Init() tea.Cmd {
	log.Printf("machine: init")
	return m.children.Init()
}

func (m *machineSelectorShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	log.Printf("machine: update")
	switch msg := msg.(type) {

	case *SelectedAccountMsg:
		log.Printf("machine: got account selected orgID=%d", msg.Account.Id)
		return m, listMachinesCmd(m.ctx, m.accountClient, m.state)
	case listMachineMsg:
		m.machines = msg.machines
		m.nextCursor = msg.next
		rows := make([]table.Row, 0, len(m.machines))
		for _, org := range m.machines {
			rows = append(rows, table.Row{
				org.Name,
			})
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
				for _, item := range m.machines {
					if item.Name == selectName {
						m.selected = item
						break
					}
				}
				return m, writeSelectedMachineToState(m.state, m.selected)
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

func (m *machineSelectorShell) View() string {
	log.Printf("machine: view")
	if m.selected == nil {
		return m.appStyle.Render(
			"Select a machine\n",
			m.table.View(),
		)
	}
	return m.children.View()
}

type listMachineMsg struct {
	machines []*typesv1.Machine
	next     *typesv1.Cursor
}

func listMachinesCmd(
	ctx context.Context,
	accountClient accountv1connect.AccountServiceClient,
	state *state.State,
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("machine: listMachines")
		res, err := accountClient.ListMachine(ctx, connect.NewRequest(&accountv1.ListMachineRequest{
			Cursor:    nil,
			Limit:     10,
			AccountId: *state.CurrentAccountID,
		}))
		if err != nil {
			cerr := new(connect.Error)
			if errors.As(err, &cerr) {
				log.Printf("machine: listMachines err=%v", cerr)
			}
			return errMsg{err}
		}
		out := listMachineMsg{
			machines: make([]*typesv1.Machine, 0, len(res.Msg.Items)),
		}
		for _, mc := range res.Msg.Items {
			out.machines = append(out.machines, mc.Machine)
		}
		log.Printf("machine: got %d machines", len(out.machines))
		out.next = res.Msg.Next
		return out
	}
}

type SelectedMachineMsg struct {
	Machine *typesv1.Machine
}

func writeSelectedMachineToState(state *state.State, selected *typesv1.Machine) func() tea.Msg {
	return func() tea.Msg {
		log.Print("app: writeSelectedMachineToState")
		state.CurrentMachineID = &selected.Id
		if err := state.WriteBack(); err != nil {
			return errMsg{err}
		}
		return &SelectedMachineMsg{Machine: selected}
	}
}
