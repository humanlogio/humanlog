package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	queryv1 "github.com/humanlogio/api/go/svc/query/v1"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/tui/components/querybar"
	zone "github.com/lrstanley/bubblezone"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var axisStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("3")) // yellow

var labelStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("6")) // cyan

type QuerierModel struct {
	appStyle    lipgloss.Style
	ctx         context.Context
	state       *state.State
	queryClient queryv1connect.QueryServiceClient

	from time.Time
	to   time.Time

	summary  timeserieslinechart.Model
	querybar *querybar.QueryBar

	stopQuery func() tea.Msg
	sendMsg   func(tea.Msg)

	height int
	width  int
	inputs []*input
}

type input struct {
	logs     *typesv1.LogEventGroup
	viewport viewport.Model
}

func NewQuerierModel(
	appStyle lipgloss.Style,
	ctx context.Context,
	state *state.State,
	queryClient queryv1connect.QueryServiceClient,
	sendMsg func(tea.Msg),
) *QuerierModel {

	submitKey := key.NewBinding(key.WithKeys("enter"))

	qbar := querybar.NewQueryBar(submitKey, func(str string) []string { return nil })

	return &QuerierModel{
		appStyle:    appStyle,
		ctx:         ctx,
		state:       state,
		queryClient: queryClient,

		from:     time.Time{},
		to:       time.Now(),
		querybar: qbar,

		sendMsg: sendMsg,
	}
}

func (m *QuerierModel) Init() tea.Cmd {
	return nil
}

func (m *QuerierModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("querier: update: %T -> %#v", msg, msg)
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		zoneManager := zone.New()
		m.summary = timeserieslinechart.New(
			min(msg.Height, 8),
			msg.Width,
			timeserieslinechart.WithZoneManager(zoneManager),
		)
		m.summary.DrawXYAxisAndLabel()

		cmds = append(cmds, sendSummarizeCmd(m.ctx, m.queryClient, m.state, m.width, m.from, m.to))

	case *querybar.SubmitQueryMsg:
		// stop any existing running query
		if m.stopQuery != nil {
			cmds = append(cmds, m.stopQuery)
		}
		cmds = append(cmds, sendQueryCmd(m.ctx, m.queryClient, m.state, msg.Query, m.sendMsg))
		return m, tea.Batch(cmds...)
	case *StartLogStreamMsg:
		m.stopQuery = msg.StopFunc
		for _, leg := range msg.Events {
			m.addToInput(leg.MachineId, leg.SessionId, leg.Logs)
		}
	case *AppendLogsMsg:
		for _, leg := range msg.Events {
			m.addToInput(leg.MachineId, leg.SessionId, leg.Logs)
		}
	case *SummaryMsg:
		m.summary.ClearAllData()
		for _, bucket := range msg.Buckets {
			tp := timeserieslinechart.TimePoint{
				Time:  bucket.Ts.AsTime(),
				Value: float64(bucket.EventCount),
			}
			m.summary.Push(tp)
		}
		m.summary.Draw()

	case errMsg:
		log.Printf("querier error: %#v", msg.err)
		return m, tea.Quit
	}
	qbar, qcmd := m.querybar.Update(msg)
	m.querybar = qbar.(*querybar.QueryBar)

	if qcmd != nil {
		cmds = append(cmds, qcmd)
	}

	for _, input := range m.inputs {
		vport, vcmd := input.viewport.Update(msg)
		input.viewport = vport
		if vcmd != nil {
			cmds = append(cmds, vcmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *QuerierModel) addToInput(machineID, sessionID int64, logs []*typesv1.LogEvent) {
	if m.inputs == nil {
		m.inputs = make([]*input, 0)
	}
	found := false
	for _, input := range m.inputs {
		if input.logs.MachineId != machineID {
			continue
		}
		if input.logs.SessionId != sessionID {
			continue
		}
		input.logs.Logs = append(input.logs.Logs, logs...)
	}
	if !found {
		m.inputs = append(m.inputs, &input{
			viewport: viewport.New(0, 0), // will be set to the proper w & h right away
			logs: &typesv1.LogEventGroup{
				MachineId: machineID,
				SessionId: sessionID,
				Logs:      logs,
			},
		})
		// resize all inputs to the right w & h
		m.resizeViewports()
	}
}

func (m *QuerierModel) resizeViewports() {
	height := m.height - lipgloss.Height(m.querybar.View())
	width := m.width / len(m.inputs)
	for _, sessions := range m.inputs {
		sessions.viewport.Height = height
		sessions.viewport.Width = width
	}
}

func sendSummarizeCmd(
	ctx context.Context,
	client queryv1connect.QueryServiceClient,
	state *state.State,
	width int,
	from, to time.Time,
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("querier: send SummarizeEvents")
		res, err := client.SummarizeEvents(ctx, connect.NewRequest(&queryv1.SummarizeEventsRequest{
			AccountId:   *state.CurrentAccountID,
			BucketCount: uint32(width - 5),
			From:        timestamppb.New(from),
			To:          timestamppb.New(to),
		}))
		if err != nil {
			return errMsg{err: err}
		}
		return SummaryMsg{res.Msg}
	}
}

type SummaryMsg struct {
	*queryv1.SummarizeEventsResponse
}

func sendQueryCmd(
	ctx context.Context,
	client queryv1connect.QueryServiceClient,
	state *state.State,
	query *typesv1.LogQuery,
	sendMsg func(tea.Msg),
) func() tea.Msg {
	return func() tea.Msg {
		log.Printf("querier: send WatchQuery")
		res, err := client.WatchQuery(ctx, connect.NewRequest(&queryv1.WatchQueryRequest{
			AccountId: *state.CurrentAccountID,
			Query:     query,
		}))
		if err != nil {
			return errMsg{err: err}
		}

		if !res.Receive() {
			return nil
		}

		msg := res.Msg()

		f, err := os.Create("logdump.json")
		if err != nil {
			return errMsg{err: err}
		}
		enc := json.NewEncoder(f)

		run := true
		go func() {
			for res.Receive() && run {
				msg := res.Msg()

				for _, leg := range msg.Events {
					enc.Encode(map[string]int64{
						"machine": leg.MachineId,
						"session": leg.SessionId,
					})
					for _, ev := range leg.Logs {
						enc.Encode(ev)
					}
				}
				sendMsg(&AppendLogsMsg{
					Events: msg.GetEvents(),
				})
			}
			if err := res.Err(); err != nil {
				sendMsg(errMsg{err: err})
			}
		}()

		return &StartLogStreamMsg{
			Events: msg.GetEvents(),
			StopFunc: func() tea.Msg {
				f.Close()
				err := res.Close()
				run = false
				if err != nil {
					return errMsg{err: err}
				}
				return nil
			},
		}
	}
}

type StartLogStreamMsg struct {
	Events   []*typesv1.LogEventGroup
	StopFunc func() tea.Msg
}

type AppendLogsMsg struct {
	Events []*typesv1.LogEventGroup
}

func (m *QuerierModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}
	var sessions []string
	for _, input := range m.inputs {
		input.viewport.SetContent(renderLogs(input.logs.Logs))
		sessions = append(sessions, input.viewport.View())
	}
	return m.appStyle.Render(lipgloss.JoinVertical(
		lipgloss.Left,
		m.summary.View(),
		m.querybar.View(),
		lipgloss.JoinHorizontal(lipgloss.Top, sessions...),
	))
}

func renderLogs(logs []*typesv1.LogEvent) string {
	return fmt.Sprintf("%d log events", len(logs))
}
