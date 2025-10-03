package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	"github.com/humanlogio/api/go/svc/project/v1/projectv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const (
	projectCmdName = "project"
)

func projectCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {

	var (
		localhostFlag     = cli.BoolFlag{Name: "localhost"}
		dashboardsDirFlag = cli.StringFlag{Name: "dashboards", Value: "tel/dashboards"}
		alertsDirFlag     = cli.StringFlag{Name: "alerts", Value: "tel/alerts"}
	)
	getProjectClient := func(cctx *cli.Context, useLocalhost bool) (_ projectv1connect.ProjectServiceClient, envID int64, _ error) {
		ctx := getCtx(cctx)
		ll := getLogger(cctx)
		state := getState(cctx)
		clOpts := getConnectOpts(cctx)
		ll.InfoContext(ctx, "localhost value", slog.Bool("localhost", useLocalhost))
		var (
			apiURL     string
			httpClient *http.Client
		)
		if !useLocalhost {
			tokenSource := getTokenSource(cctx)
			clOpts = append(clOpts, connect.WithInterceptors(
				auth.Interceptors(ll, tokenSource)...,
			))
			apiURL = getAPIUrl(cctx)
			httpClient = getHTTPClient(cctx, apiURL)
			environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, clOpts)
			if err != nil {
				return nil, 0, fmt.Errorf("selecting environment to interact with")
			}
			envID = environmentID
		} else {
			cfg := getCfg(cctx)
			expcfg := cfg.GetRuntime().GetExperimentalFeatures()
			if expcfg == nil || expcfg.ServeLocalhost == nil {
				return nil, 0, fmt.Errorf("localhost feature is not enabled or not configured, can't dial localhost")
			}
			apiURL = fmt.Sprintf("http://localhost:%d", expcfg.ServeLocalhost.Port)
			httpClient = getHTTPClient(cctx, apiURL)
		}

		return projectv1connect.NewProjectServiceClient(httpClient, apiURL, clOpts...), envID, nil
	}

	return cli.Command{
		Hidden: hideUnreleasedFeatures == "true",
		Name:   projectCmdName,
		Usage:  "Manage projects.",
		Flags:  []cli.Flag{localhostFlag},
		Subcommands: []cli.Command{
			{
				Name:      "init",
				Usage:     "initialize a local project in the current directory",
				ArgsUsage: "<name> <path>",
				Flags:     []cli.Flag{dashboardsDirFlag, alertsDirFlag},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)

					projectName := cctx.Args().First()
					if projectName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}
					path := cctx.Args().Get(1)
					if path == "" || path == "." {
						cwd, err := os.Getwd()
						if err != nil {
							return err
						}
						path = cwd
					}
					dashboardDir := cctx.String(dashboardsDirFlag.Name)
					alertDir := cctx.String(alertsDirFlag.Name)
					projectClient, envID, err := getProjectClient(cctx, true)
					if err != nil {
						return fmt.Errorf("preparing project client: %v", err)
					}

					pointer := &typesv1.ProjectPointer{}

					pointer.Scheme = &typesv1.ProjectPointer_Localhost{
						Localhost: &typesv1.ProjectPointer_LocalGit{
							Path:         path,
							DashboardDir: dashboardDir,
							AlertDir:     alertDir,
							ReadOnly:     false,
						},
					}

					createRes, err := projectClient.CreateProject(ctx, connect.NewRequest(&projectv1.CreateProjectRequest{
						EnvironmentId: envID,
						Spec: &typesv1.ProjectSpec{
							Name:    projectName,
							Pointer: pointer,
						},
					}))
					if err != nil {
						return fmt.Errorf("creating project: %v", err)
					}
					project := createRes.Msg.Project
					printProject(project)

					return nil
				},
			},
			{
				Name:      "track-remote",
				Usage:     "track a remote project",
				ArgsUsage: "<name> <url> <ref>",
				Flags:     []cli.Flag{dashboardsDirFlag, alertsDirFlag},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)

					projectName := cctx.Args().First()
					if projectName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}
					url := cctx.Args().Get(1)
					if url == "" {
						logerror("missing argument: <url>")
						return cli.ShowSubcommandHelp(cctx)
					}
					ref := cctx.Args().Get(2)
					if ref == "" {
						logerror("missing argument: <ref>")
						return cli.ShowSubcommandHelp(cctx)
					}

					dashboardDir := cctx.String(dashboardsDirFlag.Name)
					alertDir := cctx.String(alertsDirFlag.Name)
					projectClient, envID, err := getProjectClient(cctx, true)
					if err != nil {
						return fmt.Errorf("preparing project client: %v", err)
					}

					pointer := &typesv1.ProjectPointer{}

					pointer.Scheme = &typesv1.ProjectPointer_Remote{
						Remote: &typesv1.ProjectPointer_RemoteGit{
							RemoteUrl:    url,
							Ref:          ref,
							DashboardDir: dashboardDir,
							AlertDir:     alertDir,
						},
					}

					createRes, err := projectClient.CreateProject(ctx, connect.NewRequest(&projectv1.CreateProjectRequest{
						EnvironmentId: envID,
						Spec: &typesv1.ProjectSpec{
							Name:    projectName,
							Pointer: pointer,
						},
					}))
					if err != nil {
						return fmt.Errorf("creating project: %v", err)
					}
					project := createRes.Msg.Project
					printProject(project)

					return nil
				},
			},
			{
				Name:  "get",
				Usage: "get a specific project in an environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					projectName := cctx.Args().First()
					if projectName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}
					projectClient, envID, err := getProjectClient(cctx, cctx.GlobalBool(localhostFlag.Name))
					if err != nil {
						return fmt.Errorf("preparing project client: %v", err)
					}

					getRes, err := projectClient.GetProject(ctx, connect.NewRequest(&projectv1.GetProjectRequest{
						EnvironmentId: envID,
						Name:          projectName,
					}))
					if err != nil {
						return fmt.Errorf("retrieving project: %v", err)
					}
					msg := getRes.Msg
					printProject(msg.Project)
					for _, db := range msg.Dashboards {
						printDashboard(db)
					}
					for _, ag := range msg.AlertGroups {
						printAlertGroup(ag)
					}
					return nil
				},
			},
			{
				Name:  "delete",
				Usage: "delete a specific project in an environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					projectName := cctx.Args().First()
					if projectName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}
					projectClient, envID, err := getProjectClient(cctx, cctx.GlobalBool(localhostFlag.Name))
					if err != nil {
						return fmt.Errorf("preparing project client: %v", err)
					}

					deleteRes, err := projectClient.DeleteProject(ctx, connect.NewRequest(&projectv1.DeleteProjectRequest{
						EnvironmentId: envID,
						Name:          projectName,
					}))
					if err != nil {
						return fmt.Errorf("retrieving project: %v", err)
					}
					_ = deleteRes
					loginfo("project %q was deleted", projectName)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "list the projects in an environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)

					projectClient, envID, err := getProjectClient(cctx, cctx.GlobalBool(localhostFlag.Name))
					if err != nil {
						return fmt.Errorf("preparing project client: %v", err)
					}

					anyProject := false
					iter := ListProjects(ctx, envID, projectClient)
					for iter.Next() {
						anyProject = true
						project := iter.Current().Project
						printProject(project)
					}
					if err := iter.Err(); err != nil {
						return fmt.Errorf("listing projects: %v", err)
					}
					if !anyProject {
						loginfo("no project found")
					}
					return nil
				},
			},
		},
	}
}

func printProject(project *typesv1.Project) {
	printFact("name", project.Spec.Name)
	switch scheme := project.Spec.Pointer.Scheme.(type) {
	case *typesv1.ProjectPointer_Remote:
		printFact("type", "remote")
		printFact("type.remote.ref", scheme.Remote.Ref)
		printFact("type.remote.url", scheme.Remote.RemoteUrl)
		printFact("type.remote.dashboards", scheme.Remote.DashboardDir)
		printFact("type.remote.alerts", scheme.Remote.AlertDir)
	case *typesv1.ProjectPointer_Localhost:
		printFact("type", "local")
		printFact("type.local.path", scheme.Localhost.Path)
		printFact("type.local.readonly", scheme.Localhost.ReadOnly)
		printFact("type.local.dashboards", scheme.Localhost.DashboardDir)
		printFact("type.local.alerts", scheme.Localhost.AlertDir)
	case *typesv1.ProjectPointer_Db:
		printFact("type", "db")
		printFact("type.db.uri", scheme.Db.Uri)
	default:
		logerror("unexpected project pointer type: %T. this is a bug in humanlog, please report it", scheme)
	}
}

func printDashboard(dashboard *typesv1.Dashboard) {
	printFact("id", dashboard.Meta.Id)
	printFact("name", dashboard.Spec.Name)
	printFact("description", dashboard.Spec.Description)
	printFact("is_readonly", dashboard.Spec.IsReadonly)
	printFact("created_at", dashboard.Status.CreatedAt)
	printFact("updated_at", dashboard.Status.UpdatedAt)
}

func printAlertGroup(alertGroup *typesv1.AlertGroup) {
	printFact("name", alertGroup.Spec.Name)
	if alertGroup.Spec.Interval == nil {
		printFact("interval", "<none>")
	} else {
		printFact("interval", alertGroup.Spec.Interval.AsDuration())
	}
	if alertGroup.Spec.QueryOffset == nil {
		printFact("query_offset", "<none>")
	} else {
		printFact("query_offset", alertGroup.Spec.QueryOffset.AsDuration())
	}

	printFact("limit", alertGroup.Spec.Limit)
	printFact("len(rules)", len(alertGroup.Spec.Rules))
	printFact("labels", alertGroup.Spec.Labels)
}
