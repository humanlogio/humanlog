package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/charmbracelet/huh"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const (
	organizationCmdName = "organization"
)

func organizationCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(*cli.Context) *http.Client,
) cli.Command {

	var (
		createOrgNameFlag = cli.StringFlag{
			Name:  "name",
			Usage: "name of the org to create",
		}
	)

	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      organizationCmdName,
		ShortName: "org",
		Usage:     "Manage organizations for the current user.",
		Before: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
			if err != nil {
				return err
			}
			return nil
		},
		Subcommands: []cli.Command{
			{
				Name:      "set-current",
				Usage:     "set the org currently configured in the CLI",
				ArgsUsage: "<name>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					organizationName := cctx.Args().First()
					if organizationName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)

					iter := ListOrganizations(ctx, userClient)

					for iter.Next() {
						li := iter.Current()
						if li.Organization.Name != organizationName {
							continue
						}

						state.CurrentOrgID = &li.Organization.Id
						return state.WriteBack()
					}
					if err := iter.Err(); err != nil {
						return err
					}
					return fmt.Errorf("you're not part of any org with name %q", organizationName)
				},
			},
			{
				Name:  "get-current",
				Usage: "get the org currently configured in the CLI",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					if state.CurrentOrgID == nil {
						return fmt.Errorf("no org is currently set")
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)

					iter := ListOrganizations(ctx, userClient)

					for iter.Next() {
						li := iter.Current()
						if li.Organization.Id != *state.CurrentOrgID {
							continue
						}
						org := li.Organization

						printFact("org id", org.Id)
						printFact("org name", org.Name)
						printFact("created on", org.CreatedAt.AsTime())
						return nil
					}
					if err := iter.Err(); err != nil {
						return err
					}
					return fmt.Errorf("current org not found")
				},
			},
			{
				Name:  "switch",
				Usage: "switch to a different org. like `set-current` but with a prompt",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)
					orgID, err := huhSelectOrganizations(ctx, userClient, "Which org do you want to switch to?")
					if err != nil {
						return err
					}
					state.CurrentOrgID = &orgID
					return state.WriteBack()
				},
			},
			{
				Name:  "create",
				Usage: "create an org",
				Flags: []cli.Flag{createOrgNameFlag},
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)

					req := &userv1.CreateOrganizationRequest{
						Name: cctx.String(createOrgNameFlag.Name),
					}

					if req.Name == "" {
						err := huh.NewInput().
							Title("How should this org be named?").
							Value(&req.Name).
							WithTheme(huhTheme).
							Run()
						if err != nil {
							return fmt.Errorf("requesting name from user: %v", err)
						}
					}

					res, err := userClient.CreateOrganization(ctx, connect.NewRequest(req))
					if err != nil {
						return err
					}
					org := res.Msg.Organization
					printFact("id", org.Id)
					printFact("name", org.Name)
					printFact("created at", org.CreatedAt.AsTime())
					return nil
				},
			},
			{
				Name:      "get",
				Usage:     "get an org's details",
				ArgsUsage: "<name>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					organizationName := cctx.Args().First()
					if organizationName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)

					el, ok, err := iterapi.Find(ListOrganizations(ctx, userClient), func(el *userv1.ListOrganizationResponse_ListItem) bool {
						return el.Organization.Name == organizationName
					})
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("no org with name %q", organizationName)
					}
					printFact("id", el.Organization.Id)
					printFact("name", el.Organization.Name)
					printFact("created at", el.Organization.CreatedAt.AsTime())
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "list the orgs you belong to",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)

					iter := ListOrganizations(ctx, userClient)

					for iter.Next() {
						li := iter.Current()
						org := li.Organization
						printFact("name", org.Name)
						return nil
					}
					if err := iter.Err(); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:  "list-users",
				Usage: "list the users in an org you belong to",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					organizationClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
					iter := ListOrgUser(ctx, orgID, organizationClient)
					for iter.Next() {
						u := iter.Current().User
						printFact("id", u.Id)
						printFact("email", u.Email)
					}
					if err := iter.Err(); err != nil {
						return err
					}

					return nil
				},
			},
			{
				Name:  "invite",
				Usage: "invite someone to access an org",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					_ = ctx
					_ = state
					_ = tokenSource
					_ = apiURL
					_ = httpClient

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					organizationClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
					_ = organizationClient
					return nil
				},
			},
			{
				Name:  "revoke",
				Usage: "revoke someone's access to an org",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					_ = ctx
					_ = state
					_ = tokenSource
					_ = apiURL
					_ = httpClient

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					organizationClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
					_ = organizationClient
					return nil
				},
			},
		},
	}
}
