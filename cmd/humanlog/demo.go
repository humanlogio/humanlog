package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localserver"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/localstorage"
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
			ll := getLogger(cctx)
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
			registerOnCloseServer := func(srv *http.Server) {}

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

			return localserver.ServeLocalhost(ctx, ll, localhostCfg, ownVersion, app, openStorage, registerOnCloseServer,
				func(ctx context.Context, returnToURL string) error {
					return fmt.Errorf("sign-in not enabled in demo mode")
				},
				func(ctx context.Context, returnToURL string) error {
					return fmt.Errorf("sign-out not enabled in demo mode")
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
				func(ctx context.Context, cfg *typesv1.LocalhostConfig) error {
					return fmt.Errorf("set-config not enabled in demo mode")
				},
				func(ctx context.Context) (*userv1.WhoamiResponse, error) {
					return nil, fmt.Errorf("whoami not enabled in demo mode")
				},
			)
		},
	}
}

var demoLogsData = otlplogssvcpb.ExportLogsServiceRequest{}
var demoMetricsData = otlpmetricssvcpb.ExportMetricsServiceRequest{}
var demoTraceData = otlptracesvcpb.ExportTraceServiceRequest{}
