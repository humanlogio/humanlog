package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/crazy3lf/colorconv"
	"github.com/humanlogio/api/go/svc/environment/v1/environmentv1connect"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	queryv1 "github.com/humanlogio/api/go/svc/query/v1"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
	"github.com/humanlogio/humanlog/pkg/tui"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	queryCmdName = "query"
)

func queryCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	return cli.Command{
		Hidden: hideUnreleasedFeatures == "true",
		Name:   queryCmdName,
		Usage:  "Query your logs",

		Subcommands: []cli.Command{
			{
				Name: "api",
				Subcommands: []cli.Command{
					queryApiSummarizeCmd(
						getCtx,
						getLogger,
						getCfg,
						getState,
						getTokenSource,
						getAPIUrl,
						getHTTPClient,
						getConnectOpts,
					),
					queryApiRunCmd(
						getCtx,
						getLogger,
						getCfg,
						getState,
						getTokenSource,
						getAPIUrl,
						getHTTPClient,
						getConnectOpts,
					),
				},
			},
		},

		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)
			clOpts := getConnectOpts(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
			if err != nil {
				return err
			}
			ll := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
			clOpts = append(clOpts, connect.WithInterceptors(
				auth.Interceptors(ll, tokenSource)...,
			))
			return query(ctx, state, apiURL, httpClient, clOpts)
		},
	}
}

func query(
	ctx context.Context,
	state *state.State,
	apiURL string,
	httpClient *http.Client,
	clOpts []connect.ClientOption,
) error {
	var (
		userClient         = userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts...)
		organizationClient = organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts...)
		environmentClient  = environmentv1connect.NewEnvironmentServiceClient(httpClient, apiURL, clOpts...)
		queryClient        = queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts...)
	)
	return tui.RunTUI(ctx, state, userClient, organizationClient, environmentClient, queryClient)
}

func queryApiSummarizeCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {
	bucket := cli.IntFlag{Name: "buckets", Value: 20}
	fromFlag := cli.DurationFlag{Name: "since", Value: 365 * 24 * time.Hour}
	toFlag := cli.DurationFlag{Name: "to", Value: 0}
	localhost := cli.BoolFlag{Name: "localhost"}
	return cli.Command{
		Name:  "summarize",
		Flags: []cli.Flag{localhost, fromFlag, toFlag, bucket},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)

			var queryClient queryv1connect.QueryServiceClient
			if !cctx.Bool(localhost.Name) {
				ll := getLogger(cctx)
				tokenSource := getTokenSource(cctx)
				apiURL := getAPIUrl(cctx)
				httpClient := getHTTPClient(cctx, apiURL)
				clOpts := getConnectOpts(cctx)
				_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
				if err != nil {
					return err
				}
				clOpts = append(clOpts, connect.WithInterceptors(
					auth.Interceptors(ll, tokenSource)...,
				))
				queryClient = queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts...)
			} else {
				cfg := getCfg(cctx)
				expcfg := cfg.GetRuntime().GetExperimentalFeatures()
				if expcfg == nil || expcfg.ServeLocalhost == nil {
					return fmt.Errorf("localhost feature is not enabled or not configured, can't dial localhost")
				}
				apiURL := fmt.Sprintf("http://localhost:%d", expcfg.ServeLocalhost.Port)
				httpClient := getHTTPClient(cctx, apiURL)
				queryClient = queryv1connect.NewQueryServiceClient(httpClient, apiURL)
			}

			termWidth, termHeight, err := term.GetSize(os.Stdout.Fd())
			if err != nil {
				return fmt.Errorf("getting term size: %v", err)
			}
			now := time.Now()
			var (
				from *timestamppb.Timestamp
				to   *timestamppb.Timestamp
			)
			if cctx.Duration(fromFlag.Name) != 0 {
				from = timestamppb.New(now.Add(-cctx.Duration(fromFlag.Name)))
			}
			if cctx.Duration(toFlag.Name) != 0 {
				to = timestamppb.New(now.Add(-cctx.Duration(toFlag.Name)))
			}

			res, err := queryClient.SummarizeEvents(ctx, connect.NewRequest(&queryv1.SummarizeEventsRequest{
				// EnvironmentId: *state.CurrentEnvironmentID,
				BucketCount: uint32(cctx.Int(bucket.Name)),
				From:        from,
				To:          to,
			}))
			if err != nil {
				return fmt.Errorf("querying summary data: %v", err)
			}

			buckets := res.Msg.Buckets

			firstTimeformat := "'06 01/02 15:04:05"
			width := res.Msg.BucketWidth.AsDuration()
			if width < time.Microsecond {
				firstTimeformat = "'06 01/02 15:04:05.000000000"
			} else if width < time.Millisecond {
				firstTimeformat = "'06 01/02 15:04:05.000000"
			} else if width < time.Second {
				firstTimeformat = "'06 01/02 15:04:05.000"
			} else if width > 24*time.Hour {
				firstTimeformat = "'06 01/02"
			}
			lastTimeFormat := "'06 01/02 15:04:05"
			window := to.AsTime().Sub(from.AsTime())
			if window < time.Microsecond {
				lastTimeFormat = ".000000000"
			} else if window < time.Millisecond {
				lastTimeFormat = ".000000"
			} else if window < time.Second {
				lastTimeFormat = ".000"
			} else if window < time.Minute {
				lastTimeFormat = "05s"
			} else if window < time.Hour {
				lastTimeFormat = "15:04:05"
			} else if window < 24*time.Hour {
				lastTimeFormat = "15:04"
			} else if window > 24*time.Hour {
				lastTimeFormat = "'06 01/02"
			}
			stepTimeFormat := "'06 01/02 15:04:05"
			if width < time.Microsecond {
				stepTimeFormat = ".000000000"
			} else if width < time.Millisecond {
				stepTimeFormat = ".000000"
			} else if width < time.Second {
				stepTimeFormat = ".000"
			} else if width < time.Minute {
				stepTimeFormat = "05s"
			} else if width < time.Hour {
				stepTimeFormat = "15:04:05"
			} else if width < 24*time.Hour {
				stepTimeFormat = "15:04"
			} else if width > 24*time.Hour {
				stepTimeFormat = "'06 01/02"
			}

			tslc := timeserieslinechart.New(termWidth, termHeight-3,
				timeserieslinechart.WithTimeRange(from.AsTime(), to.AsTime()),
			)
			tslc.XLabelFormatter = linechart.LabelFormatter(func(i int, f float64) string {
				t := time.Unix(int64(f), 0).UTC()
				var ts string
				if i == 0 {
					ts = t.Format(firstTimeformat)
				} else if i == len(buckets)-1 {
					ts = t.Format(lastTimeFormat)
				} else {
					ts = t.Format(stepTimeFormat)
				}
				loginfo("label: ts=%v", ts)
				return ts
			})
			for _, bucket := range buckets {
				loginfo("ts=%v   ev=%d", bucket.Ts.AsTime().Format(time.RFC3339Nano), bucket.GetEventCount())
				tslc.Push(timeserieslinechart.TimePoint{
					Time:  bucket.Ts.AsTime(),
					Value: float64(bucket.GetEventCount()),
				})
			}
			tslc.SetLineStyle(runes.ThinLineStyle)
			tslc.Draw()

			fmt.Fprint(os.Stdout, tslc.View())

			return nil
		},
	}
}

func queryApiRunCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	fromFlag := cli.DurationFlag{Name: "since", Value: 365 * 24 * time.Hour}
	toFlag := cli.DurationFlag{Name: "to", Value: 0}
	localhost := cli.BoolFlag{Name: "localhost"}
	format := cli.StringFlag{Name: "format", Value: "humanlog"}
	limit := cli.IntFlag{Name: "limit", Value: 10}
	return cli.Command{
		Name:  "run",
		Flags: []cli.Flag{localhost, fromFlag, toFlag, format, limit},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			cfg := getCfg(cctx)
			state := getState(cctx)
			var queryClient queryv1connect.QueryServiceClient
			if !cctx.Bool(localhost.Name) {
				ll := getLogger(cctx)
				tokenSource := getTokenSource(cctx)
				apiURL := getAPIUrl(cctx)
				httpClient := getHTTPClient(cctx, apiURL)
				clOpts := getConnectOpts(cctx)
				_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
				if err != nil {
					return err
				}
				clOpts = append(clOpts, connect.WithInterceptors(
					auth.Interceptors(ll, tokenSource)...,
				))
				queryClient = queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts...)
			} else {
				cfg := getCfg(cctx)
				expcfg := cfg.GetRuntime().GetExperimentalFeatures()
				if expcfg == nil || expcfg.ServeLocalhost == nil {
					return fmt.Errorf("localhost feature is not enabled or not configured, can't dial localhost")
				}
				apiURL := fmt.Sprintf("http://localhost:%d", expcfg.ServeLocalhost.Port)
				httpClient := getHTTPClient(cctx, apiURL)
				queryClient = queryv1connect.NewQueryServiceClient(httpClient, apiURL)
			}
			now := time.Now()
			var (
				from  *timestamppb.Timestamp
				to    *timestamppb.Timestamp
				query = strings.Join(cctx.Args(), " ")
			)
			if cctx.Duration(fromFlag.Name) != 0 {
				from = timestamppb.New(now.Add(-cctx.Duration(fromFlag.Name)))
			}
			if cctx.Duration(toFlag.Name) != 0 {
				to = timestamppb.New(now.Add(-cctx.Duration(toFlag.Name)))
			}
			sinkOpts, errs := stdiosink.StdioOptsFrom(cfg.GetFormatter())
			if len(errs) > 0 {
				for _, err := range errs {
					logerror("config error: %v", err)
				}
			}

			loginfo("from=%s", from)
			loginfo("to=%s", to)
			loginfo("query=%s", query)

			var environmentID int64
			if state.CurrentEnvironmentID != nil {
				environmentID = *state.CurrentEnvironmentID
			}

			start := time.Now()
			parseRes, err := queryClient.Parse(ctx, connect.NewRequest(&queryv1.ParseRequest{Query: query}))
			if err != nil {
				return fmt.Errorf("parsing query: %v", err)
			}
			parsedIn := time.Since(start)
			lq := parseRes.Msg.Query

			if lq.Timerange == nil {
				lq.Timerange = new(typesv1.Timerange)
			}
			if lq.Timerange.From == nil && from != nil {
				lq.Timerange.From = typesv1.ExprLiteral(typesv1.ValTimestamp(from))
			}
			if lq.Timerange.To == nil && to != nil {
				lq.Timerange.To = typesv1.ExprLiteral(typesv1.ValTimestamp(to))
			}

			var printer func(*typesv1.Data) error
			switch cctx.String(format.Name) {
			case "json":

				printer = func(data *typesv1.Data) error {
					b, err := protojson.Marshal(data)
					if err != nil {
						return fmt.Errorf("marshaling: %v", err)
					}
					_, err = os.Stdout.Write(b)
					_, _ = os.Stdout.WriteString("\n")
					return err
				}
			case "humanlog":

				sink, err := stdiosink.NewStdio(os.Stdout, sinkOpts)
				if err != nil {
					return fmt.Errorf("preparing stdio printer: %v", err)
				}
				printer = func(data *typesv1.Data) error {
					var events []*typesv1.IngestedLogEvent
					switch shape := data.Shape.(type) {
					case *typesv1.Data_Tabular:
						switch tshape := shape.Tabular.Shape.(type) {
						case *typesv1.Tabular_LogEvents:
							events = tshape.LogEvents.Events
						default:
							return fmt.Errorf("todo: handle data shape %T", tshape)
						}
					default:
						return fmt.Errorf("todo: handle data shape %T", shape)
					}

					for _, ev := range events {
						prefix := getPrefix(ev.MachineId, ev.SessionId)
						postProcess := func(pattern string) string {
							return prefix + pattern
						}
						ev := &typesv1.LogEvent{
							ParsedAt:   ev.ParsedAt,
							Raw:        ev.Raw,
							Structured: ev.Structured,
						}
						if err := sink.ReceiveWithPostProcess(ctx, ev, postProcess); err != nil {
							return fmt.Errorf("printing log: %v", err)
						}
					}
					return nil
				}
			default:
				return fmt.Errorf("unsupported format: %q", cctx.String(format.Name))
			}

			var (
				limit = int32(cctx.Int(limit.Name))
				req   = &queryv1.QueryRequest{
					EnvironmentId: environmentID,
					Query:         lq,
					Cursor:        nil,
					Limit:         limit,
				}
			)
			var (
				totalQueryTime time.Duration
				queriedInAll   []time.Duration
			)
			for {

				start := time.Now()
				res, err := queryClient.Query(ctx, connect.NewRequest(req))
				if err != nil {
					return fmt.Errorf("calling Query: %v", err)
				}
				queriedIn := time.Since(start)
				totalQueryTime += queriedIn
				queriedInAll = append(queriedInAll, queriedIn)
				data := res.Msg.Data
				if err := printer(data); err != nil {
					return fmt.Errorf("printing data: %v", err)
				}
				if res.Msg.Next == nil {
					break
				}
				req.Cursor = res.Msg.Next
			}

			var queriedAllDur []string
			for _, queriedIn := range queriedInAll {
				queriedAllDur = append(queriedAllDur, queriedIn.String())
			}

			_ = json.NewEncoder(os.Stderr).Encode(map[string]any{
				"parsed_in":        parsedIn.String(),
				"pages":            len(queriedInAll),
				"total_query_time": totalQueryTime.String(),
				"all_query_times":  queriedAllDur,
			})

			return nil
		},
	}
}

type tuple struct{ m, s int64 }

var colorPrefixes = map[tuple]string{}

func getPrefix(machine, session int64) string {
	prefix, ok := colorPrefixes[tuple{m: machine, s: session}]
	if ok {
		return prefix
	}
	s := lipgloss.NewStyle().
		BorderStyle(lipgloss.DoubleBorder()).BorderRight(true)

	mPrefix := s.Background(lipgloss.AdaptiveColor{
		Light: int64toLightRGB(machine),
		Dark:  int64toDarkRGB(machine),
	}).Render(strconv.FormatInt(machine, 10))
	sPrefix := s.Background(lipgloss.AdaptiveColor{
		Light: int64toLightRGB(session),
		Dark:  int64toDarkRGB(session),
	}).Render(strconv.FormatInt(session, 10))

	prefix = lipgloss.JoinHorizontal(lipgloss.Left, mPrefix, sPrefix)
	colorPrefixes[tuple{m: machine, s: session}] = prefix
	return prefix
}

func int64toDarkRGB(n int64) string {
	// modified from https://stackoverflow.com/a/52746259
	n = (374761397 + n*3266489917) & 0xffffffff
	n = ((n ^ n>>15) * 2246822519) & 0xffffffff
	n = ((n ^ n>>13) * 3266489917) & 0xffffffff
	n = (n ^ n>>16) >> 8

	hex := fmt.Sprintf("#%06x", n)

	// clamp the brightness
	r, g, b, err := colorconv.HexToRGB(hex)
	if err != nil {
		panic(err)
	}
	h, s, v := colorconv.RGBToHSV(r, g, b)
	if v > 0.5 {
		v -= 0.5
	}
	r, g, b, err = colorconv.HSVToRGB(h, s, v)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func int64toLightRGB(n int64) string {
	// modified from https://stackoverflow.com/a/52746259
	n = (374761397 + n*3266489917) & 0xffffffff
	n = ((n ^ n>>15) * 2246822519) & 0xffffffff
	n = ((n ^ n>>13) * 3266489917) & 0xffffffff
	n = (n ^ n>>16) >> 8

	hex := fmt.Sprintf("#%06x", n)

	// clamp the brightness
	r, g, b, err := colorconv.HexToRGB(hex)
	if err != nil {
		panic(err)
	}
	h, s, v := colorconv.RGBToHSV(r, g, b)
	if v < 0.5 {
		v += 0.5
	}
	r, g, b, err = colorconv.HSVToRGB(h, s, v)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
