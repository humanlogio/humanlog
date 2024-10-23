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
	accountCmdName = "account"
)

func accountCmd(
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
		Name:   accountCmdName,
		Usage:  "Manage accounts for the current user or org.",
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
				ArgsUsage: "<name>",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx)

					accountName := cctx.Args().First()
					if accountName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					// lookup `accountName` and set its ID in `state`
					_ = ctx
					_ = state
					_ = tokenSource
					_ = apiURL
					_ = httpClient
					_ = accountName

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
					httpClient := getHTTPClient(cctx)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}
					accountID, err := ensureAccountSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}
					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
					iter := ListAccounts(ctx, orgID, orgClient)
					a, ok, err := iterapi.Find(iter, func(el *organizationv1.ListAccountResponse_ListItem) bool {
						return el.Account.Id == accountID
					})
					if err != nil {
						return err
					}
					if !ok {
						logwarn("account with id %d doesn't exist anymore, select another one")
						state.CurrentAccountID = nil
						return state.WriteBack()
					}
					printFact("id", a.Account.Id)
					printFact("name", a.Account.Name)
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
					httpClient := getHTTPClient(cctx)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					res, err := orgClient.CreateAccount(ctx, connect.NewRequest(&organizationv1.CreateAccountRequest{
						OrganizationId: orgID,
					}))
					if err != nil {
						return err
					}
					account := res.Msg.Account
					printFact("created id", account.Id)
					printFact("created name", account.Name)
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
					httpClient := getHTTPClient(cctx)

					accountName := cctx.Args().First()
					if accountName == "" {
						logerror("missing argument: <name>")
						return cli.ShowSubcommandHelp(cctx)
					}

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					el, ok, err := iterapi.Find(ListAccounts(ctx, orgID, orgClient), func(el *organizationv1.ListAccountResponse_ListItem) bool {
						return el.Account.Name == accountName
					})
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("no account with name %q", accountName)
					}
					printFact("id", el.Account.Id)
					printFact("name", el.Account.Name)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "list the accounts for the current user or org",
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
					orgClient := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)

					orgID, err := ensureOrgSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient)
					if err != nil {
						return err
					}

					iter := ListAccounts(ctx, orgID, orgClient)

					for iter.Next() {
						li := iter.Current()
						account := li.Account
						printFact("account name", account.Name)
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
				Usage: "generate an API token for the current account",
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
					accountID, err := ensureAccountSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}
					expiresAt, err := hubAskTokenExpiry("Creating an account token")
					if err != nil {
						return err
					}
					roles, err := hubAskTokenRoles("Creating an account token")
					if err != nil {
						return err
					}
					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					res, err := tokenClient.GenerateAccountToken(ctx, connect.NewRequest(&tokenv1.GenerateAccountTokenRequest{
						AccountId: accountID,
						ExpiresAt: timestamppb.New(expiresAt),
						Roles:     roles,
					}))
					if err != nil {
						return fmt.Errorf("generating account token: %v", err)
					}
					token := res.Msg.Token
					printFact("id", token.TokenId)
					printFact("account id", token.AccountId)
					printFact("expires at", token.ExpiresAt.AsTime())
					printFact("roles", token.Roles)
					printFact("token (secret! do not lose)", token.Token)
					return nil
				},
			},
			{
				Name:      "revoke-token",
				Usage:     "revoke an API token for the current account",
				ArgsUsage: "<token id>",
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
					accountID, err := ensureAccountSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
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

					loginfo("revoking token %d on account %d", tokenID, accountID)
					res, err := tokenClient.RevokeAccountToken(ctx, connect.NewRequest(&tokenv1.RevokeAccountTokenRequest{
						AccountId: accountID,
						TokenId:   tokenID,
					}))
					if err != nil {
						return fmt.Errorf("revoking account token: %v", err)
					}
					_ = res
					loginfo("token revoked")
					return nil
				},
			},
			{
				Name:      "view-token",
				Usage:     "view the details of an API token for the current account",
				ArgsUsage: "<token id>",
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
					accountID, err := ensureAccountSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
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

					res, err := tokenClient.GetAccountToken(ctx, connect.NewRequest(&tokenv1.GetAccountTokenRequest{
						AccountId: accountID,
						TokenId:   tokenID,
					}))
					if err != nil {
						return fmt.Errorf("revoking account token: %v", err)
					}
					token := res.Msg.Token
					printFact("id", token.TokenId)
					printFact("account id", token.AccountId)
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
				Usage: "list the API tokens for the current account",
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
					accountID, err := ensureAccountSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
					if err != nil {
						return err
					}

					clOpts := connect.WithInterceptors(
						auth.Interceptors(ll, tokenSource)...,
					)

					tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

					hasAny := false
					iter := ListAccountTokens(ctx, accountID, tokenClient)
					for iter.Next() {
						hasAny = true
						token := iter.Current().Token
						printFact("id", token.TokenId)
					}
					if err := iter.Err(); err != nil {
						return fmt.Errorf("listing tokens: %v", err)
					}
					if !hasAny {
						loginfo("no account token found")
					}

					return nil
				},
			},
		},
	}
}

func promptCreateAccount(ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
	orgID int64) (int64, error) {
	panic("todo")
}
