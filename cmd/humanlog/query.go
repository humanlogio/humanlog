package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/x/term"
	"github.com/humanlogio/api/go/svc/account/v1/accountv1connect"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	queryv1 "github.com/humanlogio/api/go/svc/query/v1"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/tui"
	"github.com/urfave/cli"
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
	getHTTPClient func(*cli.Context) *http.Client,
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
					),
					queryApiWatchCmd(
						getCtx,
						getLogger,
						getCfg,
						getState,
						getTokenSource,
						getAPIUrl,
						getHTTPClient,
					),
				},
			},
		},

		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
			if err != nil {
				return err
			}
			ll := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
			clOpts := connect.WithInterceptors(
				auth.Interceptors(ll, tokenSource)...,
			)
			return query(ctx, state, apiURL, httpClient, clOpts)
		},
	}
}

func query(
	ctx context.Context,
	state *state.State,
	apiURL string,
	httpClient *http.Client,
	clOpts connect.ClientOption,
) error {
	var (
		userClient         = userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)
		organizationClient = organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
		accountClient      = accountv1connect.NewAccountServiceClient(httpClient, apiURL, clOpts)
		queryClient        = queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts)
	)
	return tui.RunTUI(ctx, state, userClient, organizationClient, accountClient, queryClient)
}

func queryApiSummarizeCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(*cli.Context) *http.Client,
) cli.Command {
	fromFlag := cli.DurationFlag{Name: "since", Value: 365 * 24 * time.Hour}
	toFlag := cli.DurationFlag{Name: "to", Value: 0}
	return cli.Command{
		Name:  "summarize",
		Flags: []cli.Flag{fromFlag, toFlag},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			ll := getLogger(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
			if err != nil {
				return err
			}

			termWidth, termHeight, err := term.GetSize(os.Stdout.Fd())
			if err != nil {
				return fmt.Errorf("getting term size: %v", err)
			}
			now := time.Now()
			from := now.Add(-cctx.Duration(fromFlag.Name))
			to := now.Add(-cctx.Duration(toFlag.Name))

			clOpts := connect.WithInterceptors(
				auth.Interceptors(ll, tokenSource)...,
			)
			queryClient := queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts)

			res, err := queryClient.SummarizeEvents(ctx, connect.NewRequest(&queryv1.SummarizeEventsRequest{
				AccountId:   *state.AccountID,
				BucketCount: 20,
				From:        timestamppb.New(from),
				To:          timestamppb.New(to),
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
			window := to.Sub(from)
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
				timeserieslinechart.WithTimeRange(from, to),
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
				log.Printf("label: ts=%v", ts)
				return ts
			})
			for _, bucket := range buckets {
				log.Printf("ts=%v   ev=%d", bucket.Ts.AsTime().Format(time.RFC3339Nano), bucket.GetEventCount())
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

func queryApiWatchCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(*cli.Context) *http.Client,
) cli.Command {
	return cli.Command{
		Name: "watch",
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
			if err != nil {
				return err
			}
			ll := getLogger(cctx)
			clOpts := connect.WithInterceptors(
				auth.Interceptors(ll, tokenSource)...,
			)
			queryClient := queryv1connect.NewQueryServiceClient(httpClient, apiURL, clOpts)
			_ = queryClient
			return nil
		},
	}
}
