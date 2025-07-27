package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"connectrpc.com/connect"
	featurev1 "github.com/humanlogio/api/go/svc/feature/v1"
	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	localhostv1 "github.com/humanlogio/api/go/svc/localhost/v1"
	"github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	stackv1 "github.com/humanlogio/api/go/svc/stack/v1"
	"github.com/humanlogio/api/go/svc/stack/v1/stackv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
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
	getLocalhostHTTPClient func(cctx *cli.Context) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	return cli.Command{
		Name:   apiCmdName,
		Usage:  "Raw interactions with the API",
		Hidden: true,
		Subcommands: []cli.Command{
			apiLocalhost(
				getCtx,
				getLogger,
				getCfg,
				getState,
				getTokenSource,
				getAPIUrl,
				getLocalhostHTTPClient,
				getConnectOpts,
			),
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
			apiStack(
				getCtx,
				getLogger,
				getCfg,
				getState,
				getTokenSource,
				getLocalhostHTTPClient,
				getConnectOpts,
			),
		},
	}
}

func apiLocalhost(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getLocalhostHTTPClient func(cctx *cli.Context) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {

	getLocalhostClient := func(cctx *cli.Context) localhostv1connect.LocalhostServiceClient {

		cfg := getCfg(cctx)
		port := cfg.GetRuntime().GetExperimentalFeatures().GetServeLocalhost().GetPort()

		localhostAddr := net.JoinHostPort("localhost", strconv.FormatInt(port, 10))
		addr, err := url.Parse("http://" + localhostAddr)
		if err != nil {
			panic(err)
		}
		httpClient := getLocalhostHTTPClient(cctx)
		client := localhostv1connect.NewLocalhostServiceClient(httpClient, addr.String())
		return client
	}

	return cli.Command{
		Name: "localhost",
		Subcommands: []cli.Command{
			{
				Name: "stats",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getLocalhostClient(cctx)
					ll.InfoContext(ctx, "looking localhost stats")
					req := &localhostv1.GetStatsRequest{}
					res, err := client.GetStats(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					return err
				},
			},
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

func apiStack(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getLocalhostHTTPClient func(cctx *cli.Context) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {

	getStackClient := func(cctx *cli.Context) stackv1connect.StackServiceClient {
		cfg := getCfg(cctx)
		port := cfg.GetRuntime().GetExperimentalFeatures().GetServeLocalhost().GetPort()

		localhostAddr := net.JoinHostPort("localhost", strconv.FormatInt(port, 10))
		addr, err := url.Parse("http://" + localhostAddr)
		if err != nil {
			panic(err)
		}
		httpClient := getLocalhostHTTPClient(cctx)
		ll := getLogger(cctx)
		tokenSource := getTokenSource(cctx)
		authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)
		client := stackv1connect.NewStackServiceClient(httpClient, addr.String(), authedClOpts)
		return client
	}

	var (
		stackName         = cli.StringFlag{Name: "name", Usage: "name of the stack"}
		stackPath         = cli.StringFlag{Name: "path", Usage: "where the stack's base path starts"}
		stackDashboardDir = cli.StringFlag{Name: "dashboards", Usage: "where the stack's dashboards are found (a directory)"}
		stackAlertDir     = cli.StringFlag{Name: "alerts", Usage: "where the stack's alerts are found (a directory)"}
	)

	return cli.Command{
		Name: "stack",
		Subcommands: []cli.Command{
			{
				Name: "create",
				Flags: []cli.Flag{
					stackName, stackPath, stackDashboardDir, stackAlertDir,
				},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getStackClient(cctx)

					req := &stackv1.CreateStackRequest{
						Name: cctx.String(stackName.Name),
						Pointer: &typesv1.StackPointer{
							Scheme: &typesv1.StackPointer_Localhost{
								Localhost: &typesv1.StackPointer_LocalGit{
									Path:         cctx.String(stackPath.Name),
									DashboardDir: cctx.String(stackDashboardDir.Name),
									AlertDir:     cctx.String(stackAlertDir.Name),
								},
							},
						},
					}

					ll.InfoContext(ctx, "creating stack", slog.Any("pointer", req.Pointer.GetLocalhost()))
					res, err := client.CreateStack(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					return err
				},
			},
			{
				Name: "get",
				Flags: []cli.Flag{
					stackName,
				},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getStackClient(cctx)

					req := &stackv1.GetStackRequest{
						Name: cctx.String(stackName.Name),
					}

					ll.InfoContext(ctx, "getting stack", slog.Any("name", req.Name))
					res, err := client.GetStack(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(protojson.Format(res.Msg))
					return err
				},
			},
			{
				Name:  "list",
				Flags: []cli.Flag{},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getStackClient(cctx)

					stacks := iterapi.New(ctx, 10, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*stackv1.ListStackResponse_ListItem, *typesv1.Cursor, error) {
						req := &stackv1.ListStackRequest{Cursor: cursor, Limit: limit}
						res, err := client.ListStack(ctx, connect.NewRequest(req))
						if err != nil {
							return nil, nil, err
						}
						return res.Msg.Items, res.Msg.Next, nil
					})

					ll.InfoContext(ctx, "listing stacks")
					enc := protojson.MarshalOptions{Multiline: false}
					os.Stdout.WriteString("[\n")
					first := true
					for stacks.Next() {
						if first {
							first = false
						} else {
							os.Stdout.WriteString(",\n")
						}
						os.Stdout.WriteString("\t")
						st := stacks.Current()
						_, err := os.Stdout.WriteString(enc.Format(st))
						if err != nil {
							return err
						}
					}
					os.Stdout.WriteString("\n")
					os.Stdout.WriteString("]\n")
					if err := stacks.Err(); err != nil {
						return err
					}

					return nil
				},
			},
		},
	}
}
