package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"connectrpc.com/connect"
	organizationv1 "github.com/humanlogio/api/go/svc/organization/v1"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	tokenv1 "github.com/humanlogio/api/go/svc/token/v1"
	"github.com/humanlogio/api/go/svc/token/v1/tokenv1connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	environmentCmdName = "environment"
)

func environmentCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
) cli.Command {
	return cli.Command{
		Hidden: hideUnreleasedFeatures == "true",
		Name:   environmentCmdName,
		Usage:  "Manage environments for the current user or org.",
		Before: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
			if err != nil {
				return err
			}
			return nil
		},
		Subcommands: []cli.Command{
			{
				Name:      "set-current",
				ArgsUsage: "<name>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					environmentName := cctx.Args().First()
					if environmentName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					// lookup `environmentName` and set its ID in `state`
					_ = ctx
					_ = state
					_ = tokenSource
					_ = apiURL
					_ = httpClient
					_ = environmentName

					return nil
				},
			},
			{
				Name: "get-current",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}
					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
					iter := ListEnvironments(ctx, orgClient)
					a, ok, err := iterapi.Find(iter, func(el *organizationv1.ListEnvironmentResponse_ListItem) bool {
						return el.Environment.Id == environmentID
					})
					if err != nil {
						return err
					}
					if !ok {
						logwarn("environment with id %d doesn't exist anymore, select another one")
						state.CurrentEnvironmentID = nil
						return state.WriteBack()
					}
					printFact("id", a.Environment.Id)
					printFact("name", a.Environment.Name)
					return nil
				},
			},
			{
				Name: "create",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					_, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					res, err := orgClient.CreateEnvironment(ctx, connect.NewRequest(&organizationv1.CreateEnvironmentRequest{}))
					if err != nil {
						return err
					}
					environment := res.Msg.Environment
					printFact("created id", environment.Id)
					printFact("created name", environment.Name)
					return nil
				},
			},
			{
				Name:      "get",
				ArgsUsage: "<name>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					environmentName := cctx.Args().First()
					if environmentName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					_, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					el, ok, err := iterapi.Find(ListEnvironments(ctx, orgClient), func(el *organizationv1.ListEnvironmentResponse_ListItem) bool {
						return el.Environment.Name == environmentName
					})
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("no environment with name %q", environmentName)
					}
					printFact("id", el.Environment.Id)
					printFact("name", el.Environment.Name)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "list the environments for the current user or org",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)
					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					_, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					iter := ListEnvironments(ctx, orgClient)

					for iter.Next() {
						li := iter.Current()
						environment := li.Environment
						printFact("environment name", environment.Name)
						return nil
					}
					if err := iter.Err(); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:  "generate-token",
				Usage: "generate an API token for the current environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}
					expiresAt, err := hubAskTokenExpiry("Creating an environment token")
					if err != nil {
						return err
					}
					roles, err := hubAskTokenRoles("Creating an environment token")
					if err != nil {
						return err
					}
					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					res, err := tokenClient.GenerateEnvironmentToken(ctx, connect.NewRequest(&tokenv1.GenerateEnvironmentTokenRequest{
						EnvironmentId: environmentID,
						ExpiresAt:     timestamppb.New(expiresAt),
						Roles:         roles,
					}))
					if err != nil {
						return fmt.Errorf("generating environment token: %v", err)
					}
					token := res.Msg.Token
					printFact("id", token.TokenId)
					printFact("environment id", token.EnvironmentId)
					printFact("expires at", token.ExpiresAt.AsTime())
					printFact("roles", token.Roles)
					printFact("token (secret! do not lose)", token.Token)
					return nil
				},
			},
			{
				Name:      "revoke-token",
				Usage:     "revoke an API token for the current environment",
				ArgsUsage: "<token id>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}

					tokenIdStr := cctx.Args().First()
					if tokenIdStr == "" {
						logerror("missing argument: <token id>")
						return cli.ShowSubcommandHelp(cctx)
					}
					tokenID, err := strconv.ParseInt(tokenIdStr, 10, 64)
					if err != nil {
						logerror("invalid argument: <token id>")
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					loginfo("revoking token %d on environment %d", tokenID, environmentID)
					res, err := tokenClient.RevokeEnvironmentToken(ctx, connect.NewRequest(&tokenv1.RevokeEnvironmentTokenRequest{
						EnvironmentId: environmentID,
						TokenId:       tokenID,
					}))
					if err != nil {
						return fmt.Errorf("revoking environment token: %v", err)
					}
					_ = res
					loginfo("token revoked")
					return nil
				},
			},
			{
				Name:      "view-token",
				Usage:     "view the details of an API token for the current environment",
				ArgsUsage: "<token id>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}

					tokenIdStr := cctx.Args().First()
					if tokenIdStr == "" {
						logerror("missing argument: <token id>")
						return cli.ShowSubcommandHelp(cctx)
					}
					tokenID, err := strconv.ParseInt(tokenIdStr, 10, 64)
					if err != nil {
						logerror("invalid argument: <token id>")
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					res, err := tokenClient.GetEnvironmentToken(ctx, connect.NewRequest(&tokenv1.GetEnvironmentTokenRequest{
						EnvironmentId: environmentID,
						TokenId:       tokenID,
					}))
					if err != nil {
						return fmt.Errorf("revoking environment token: %v", err)
					}
					token := res.Msg.Token
					printFact("id", token.TokenId)
					printFact("environment id", token.EnvironmentId)
					printFact("roles", token.Roles)
					printFact("expires at", token.ExpiresAt.AsTime())
					if token.LastUsedAt != nil {
						printFact("last used at", token.LastUsedAt.AsTime())
					} else {
						printFact("last used at", "never")
					}
					if token.RevokedAt != nil {
						printFact("revoked at", token.RevokedAt.AsTime())
					} else {
						printFact("revoked at", "never")
					}
					return nil
				},
			},
			{
				Name:  "list-tokens",
				Usage: "list the API tokens for the current environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					environmentID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					hasAny := false
					iter := ListEnvironmentTokens(ctx, environmentID, tokenClient)
					for iter.Next() {
						hasAny = true
						token := iter.Current().Token
						printFact("id", token.TokenId)
					}
					if err := iter.Err(); err != nil {
						return fmt.Errorf("listing tokens: %v", err)
					}
					if !hasAny {
						loginfo("no environment token found")
					}

					return nil
				},
			},
		},
	}
}

func promptCreateEnvironment(ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
	orgID int64) (int64, error) {
	panic("todo")
}
