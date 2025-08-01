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
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	"github.com/humanlogio/api/go/svc/project/v1/projectv1connect"
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
			apiProject(
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

func apiProject(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getLocalhostHTTPClient func(cctx *cli.Context) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {

	getProjectClient := func(cctx *cli.Context) projectv1connect.ProjectServiceClient {
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
		client := projectv1connect.NewProjectServiceClient(httpClient, addr.String(), authedClOpts)
		return client
	}

	var (
		projectName         = cli.StringFlag{Name: "name", Usage: "name of the project"}
		projectPath         = cli.StringFlag{Name: "path", Usage: "where the project's base path starts"}
		projectDashboardDir = cli.StringFlag{Name: "dashboards", Usage: "where the project's dashboards are found (a directory)"}
		projectAlertDir     = cli.StringFlag{Name: "alerts", Usage: "where the project's alerts are found (a directory)"}
	)

	return cli.Command{
		Name: "project",
		Subcommands: []cli.Command{
			{
				Name: "create",
				Flags: []cli.Flag{
					projectName, projectPath, projectDashboardDir, projectAlertDir,
				},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getProjectClient(cctx)

					req := &projectv1.CreateProjectRequest{
						Name: cctx.String(projectName.Name),
						Pointer: &typesv1.ProjectPointer{
							Scheme: &typesv1.ProjectPointer_Localhost{
								Localhost: &typesv1.ProjectPointer_LocalGit{
									Path:         cctx.String(projectPath.Name),
									DashboardDir: cctx.String(projectDashboardDir.Name),
									AlertDir:     cctx.String(projectAlertDir.Name),
								},
							},
						},
					}

					ll.InfoContext(ctx, "creating project", slog.Any("pointer", req.Pointer.GetLocalhost()))
					res, err := client.CreateProject(ctx, connect.NewRequest(req))
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
					projectName,
				},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					client := getProjectClient(cctx)

					req := &projectv1.GetProjectRequest{
						Name: cctx.String(projectName.Name),
					}

					ll.InfoContext(ctx, "getting project", slog.Any("name", req.Name))
					res, err := client.GetProject(ctx, connect.NewRequest(req))
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
					client := getProjectClient(cctx)

					projects := iterapi.New(ctx, 10, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*projectv1.ListProjectResponse_ListItem, *typesv1.Cursor, error) {
						req := &projectv1.ListProjectRequest{Cursor: cursor, Limit: limit}
						res, err := client.ListProject(ctx, connect.NewRequest(req))
						if err != nil {
							return nil, nil, err
						}
						return res.Msg.Items, res.Msg.Next, nil
					})

					ll.InfoContext(ctx, "listing projects")
					enc := protojson.MarshalOptions{Multiline: false}
					os.Stdout.WriteString("[\n")
					first := true
					for projects.Next() {
						if first {
							first = false
						} else {
							os.Stdout.WriteString(",\n")
						}
						os.Stdout.WriteString("\t")
						st := projects.Current()
						_, err := os.Stdout.WriteString(enc.Format(st))
						if err != nil {
							return err
						}
					}
					os.Stdout.WriteString("\n")
					os.Stdout.WriteString("]\n")
					if err := projects.Err(); err != nil {
						return err
					}

					return nil
				},
			},
		},
	}
}
