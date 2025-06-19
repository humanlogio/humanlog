package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	queryv1 "github.com/humanlogio/api/go/svc/query/v1"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	streamCmdName = "stream"
)

func streamCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	return cli.Command{
		Name:  streamCmdName,
		Usage: "Follow a stream that matches a query",

		Subcommands: []cli.Command{
			{
				Hidden: hideUnreleasedFeatures == "true",
				Name:   "api",
				Subcommands: []cli.Command{
					streamApiRunCmd(
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
			// ctx := getCtx(cctx)
			q := strings.Join(cctx.Args(), " ")
			log.Printf("query=%q", q)
			baseSiteURL := getBaseSiteURL(cctx)

			baseSiteU, err := url.Parse(baseSiteURL)
			if err != nil {
				return fmt.Errorf("parsing base url: %v", err)
			}
			queryu := baseSiteU.JoinPath("/localhost/stream")
			v := queryu.Query()
			v.Set("query", q)
			queryu.RawQuery = v.Encode()
			return browser.OpenURL(queryu.String())
		},
	}
}

func streamApiRunCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	localhost := cli.BoolFlag{Name: "localhost"}
	format := cli.StringFlag{Name: "format", Value: "humanlog"}

	return cli.Command{
		Name:  "run",
		Flags: []cli.Flag{localhost, format},
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
			var (
				query = strings.Join(cctx.Args(), " ")
			)

			sinkOpts, errs := stdiosink.StdioOptsFrom(cfg.GetFormatter())
			if len(errs) > 0 {
				for _, err := range errs {
					logerror("config error: %v", err)
				}
			}

			loginfo("query=%s", query)

			var environmentID int64
			if state.CurrentEnvironmentID != nil {
				environmentID = *state.CurrentEnvironmentID
			}

			parseRes, err := queryClient.Parse(ctx, connect.NewRequest(&queryv1.ParseRequest{Query: query}))
			if err != nil {
				return fmt.Errorf("parsing query: %v", err)
			}
			lq := parseRes.Msg.Query

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
				printLogEvents := func(logs []*typesv1.Log) error {
					for _, ev := range logs {
						if err := sink.Receive(ctx, ev); err != nil {
							return fmt.Errorf("printing log: %v", err)
						}
					}
					return nil
				}
				printSpans := func(spans []*typesv1.Span) error {
					for _, sp := range spans {
						if err := sink.ReceiveSpan(ctx, sp); err != nil {
							return fmt.Errorf("printing span: %v", err)
						}
					}
					return nil
				}
				printTable := func(table *typesv1.Table) error {
					if err := sink.ReceiveTable(ctx, table); err != nil {
						return fmt.Errorf("printing table: %v", err)
					}
					return nil
				}
				printer = func(data *typesv1.Data) error {
					switch shape := data.Shape.(type) {

					case *typesv1.Data_Logs:
						return printLogEvents(shape.Logs.Logs)
					case *typesv1.Data_Spans:
						return printSpans(shape.Spans.Spans)
					case *typesv1.Data_FreeForm:
						return printTable(shape.FreeForm)
					default:
						return fmt.Errorf("todo: handle data shape %T", shape)
					}
				}
			default:
				return fmt.Errorf("unsupported format: %q", cctx.String(format.Name))
			}

			var (
				req = &queryv1.StreamRequest{
					EnvironmentId:  environmentID,
					Query:          lq,
					MaxBatchingFor: durationpb.New(100 * time.Millisecond),
				}
			)

			loginfo("starting to stream query=%q", query)
			streamCtx, streamCancel := context.WithCancel(ctx)
			res, err := queryClient.Stream(streamCtx, connect.NewRequest(req))
			if err != nil {
				return fmt.Errorf("calling Query: %v", err)
			}
			defer func() {
				streamCancel()
				res.Close()
			}()

			loginfo("waiting for data...")
			for res.Receive() {
				data := res.Msg().GetData()
				if err := printer(data); err != nil {
					return fmt.Errorf("printing data: %v", err)
				}
			}
			if err := res.Err(); err != nil {
				return fmt.Errorf("receiving streaming results: %v", err)
			}
			return nil
		},
	}
}
