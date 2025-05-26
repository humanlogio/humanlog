package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/charmbracelet/log"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localserver"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/types/known/structpb"

	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

const (
	demoCmdName = "demo"
)

func demoCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {
	return cli.Command{
		Name:  demoCmdName,
		Usage: "Run humanlog in demo mode.",
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			// ll := getLogger(cctx)

			handler := log.New(os.Stderr)
			handler.SetReportTimestamp(true)
			ll := slog.New(handler)

			cfg := getCfg(cctx)
			state := getState(cctx)
			ownVersion := version
			app := &localstorage.AppCtx{
				EnsureLoggedIn: func(ctx context.Context) error {
					return fmt.Errorf("please via `humanlog auth login`")
				},
				Features: nil,
				Config:   cfg,
				State:    state,
			}
			engineConfig, err := structpb.NewStruct(map[string]any{
				"path": "", // in-memory
			})
			if err != nil {
				return err
			}
			localhostCfg, err := config.GetDefaultLocalhostConfig()
			if err != nil {
				return err
			}
			port := localhostCfg.Port
			localhostCfg.Port = port + 1000          // use a higher port
			localhostCfg.EngineConfig = engineConfig // use the in-memory storage engine
			localhostCfg.Otlp = nil                  // don't ingest OTLP

			openStorage := func(ctx context.Context) (localstorage.Storage, error) {
				storage, err := localstorage.Open(
					ctx,
					localhostCfg.Engine,
					ll.WithGroup("storage"),
					localhostCfg.EngineConfig.AsMap(),
					app,
				)
				if err != nil {
					return nil, err
				}

				// seed demo data
				_, err = storage.ExportTraces(ctx, &demoTraceData)
				if err != nil {
					return nil, err
				}

				_, err = storage.ExportMetrics(ctx, &demoMetricsData)
				if err != nil {
					return nil, err
				}

				_, err = storage.ExportLogs(ctx, &demoLogsData)
				if err != nil {
					return nil, err
				}

				return storage, nil
			}

			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)

			publicClOpts := connect.WithInterceptors(auth.NewRefreshedUserAuthInterceptor(ll, tokenSource))
			authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)

			authSvc := authv1connect.NewAuthServiceClient(httpClient, apiURL, publicClOpts)
			userSvc := userv1connect.NewUserServiceClient(httpClient, apiURL, authedClOpts)

			baseSiteURL, _ := url.Parse(getBaseSiteURL(cctx))
			demoURL := baseSiteURL.JoinPath("/localhost/query")
			q := demoURL.Query()
			q.Add("demo_port", fmt.Sprintf("%d", localhostCfg.Port))
			demoURL.RawQuery = q.Encode()

			registerOnCloseServer := func(srv *http.Server) {
				// use this hook to open the browser
				time.AfterFunc(time.Second, func() {
					if err := browser.OpenURL(demoURL.String()); err != nil {
						loginfo("open your browser to interact with the demo:\n\n\t%s\n\n", demoURL.String())
					}
				})

				<-ctx.Done()
				loginfo("requesting for demo to shutdown")
				if err := srv.Close(); err != nil {
					logerror("unclean shutdown for demo server: %v", err)
				}
			}

			return localserver.ServeLocalhost(ctx, ll, localhostCfg, ownVersion, app, openStorage, registerOnCloseServer,
				func(ctx context.Context, returnToURL string) error {
					if _, err := performLoginFlow(ctx, state, authSvc, tokenSource, returnToURL); err != nil {
						return err
					}
					return nil
				},
				func(ctx context.Context, returnToURL string) error {
					if err := performLogoutFlow(ctx, userSvc, tokenSource, returnToURL); err != nil {
						return err
					}
					return nil
				},
				func(ctx context.Context) error {
					return fmt.Errorf("self-update not enabled in demo mode")
				},
				func(ctx context.Context) error {
					return fmt.Errorf("self-restart not enabled in demo mode")
				},
				func(ctx context.Context) (*typesv1.LocalhostConfig, error) {
					return cfg.CurrentConfig, nil
				},
				func(ctx context.Context, newCfg *typesv1.LocalhostConfig) error {
					cfg.CurrentConfig = newCfg
					return cfg.WriteBack()
				},
				func(ctx context.Context) (*userv1.WhoamiResponse, error) {
					cerr := new(connect.Error)
					res, err := userSvc.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
					if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
						return &userv1.WhoamiResponse{
							User: &typesv1.User{Username: "demouser"},
						}, nil
					} else if err != nil {
						return nil, fmt.Errorf("looking up user authentication status: %v", err)
					}
					return res.Msg, nil
				},
			)
		},
	}
}

var demoLogsData = otlplogssvcpb.ExportLogsServiceRequest{}
var demoMetricsData = otlpmetricssvcpb.ExportMetricsServiceRequest{}
var demoTraceData = otlptracesvcpb.ExportTraceServiceRequest{}
