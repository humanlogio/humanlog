package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	featurev1 "github.com/humanlogio/api/go/svc/feature/v1"
	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	apiCmdName = "api"
)

func apiCmd(
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
		Name:   apiCmdName,
		Usage:  "Raw interactions with the API",
		Hidden: true,
		Subcommands: []cli.Command{
			apiFeature(
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
	}
}

func apiFeature(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {

	getFeatureClient := func(cctx *cli.Context) featurev1connect.FeatureServiceClient {
		apiURL := getAPIUrl(cctx)
		httpClient := getHTTPClient(cctx, apiURL)
		ll := getLogger(cctx)
		tokenSource := getTokenSource(cctx)
		authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)
		client := featurev1connect.NewFeatureServiceClient(httpClient, apiURL, authedClOpts)
		return client
	}

	return cli.Command{
		Name: "feature",
		Subcommands: []cli.Command{
			{
				Name: "has",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getFeatureClient(cctx)

					feature := cctx.Args()[0]
					ll.InfoContext(ctx, "looking up feature access", slog.String("feature", feature))
					req := &featurev1.HasFeatureRequest{Feature: feature}
					res, err := client.HasFeature(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					return err
				},
			},
			{
				Name: "list",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					client := getFeatureClient(cctx)
					req := &featurev1.ListFeatureRequest{}
					res, err := client.ListFeature(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					return err
				},
			},
			{
				Name: "allowed-usage",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					client := getFeatureClient(cctx)
					req := &featurev1.AllowedUsageRequest{}
					res, err := client.AllowedUsage(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					// _, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					// return err
					usage := featurev1.AllowedUsageResponse_LocalhostUsage_name[int32(res.Msg.LocalhostUsage)]
					return json.NewEncoder(os.Stdout).Encode(usage)
				},
			},
		},
	}
}
