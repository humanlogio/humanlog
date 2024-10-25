package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"connectrpc.com/connect"
	"github.com/charmbracelet/huh"
	"github.com/fatih/color"
	authv1 "github.com/humanlogio/api/go/svc/auth/v1"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	organizationv1 "github.com/humanlogio/api/go/svc/organization/v1"
	"github.com/humanlogio/api/go/svc/organization/v1/organizationv1connect"
	tokenv1 "github.com/humanlogio/api/go/svc/token/v1"
	"github.com/humanlogio/api/go/svc/token/v1/tokenv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
)

func ensureLoggedIn(
	ctx context.Context,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client) (*typesv1.UserToken, error) {
	userToken, err := tokenSource.GetUserToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("looking up local user state: %v", err)
	}
	authClient := authv1connect.NewAuthServiceClient(httpClient, apiURL)
	if userToken == nil {
		confirms := false
		err := huh.NewConfirm().
			Title("You're logged out. Would you like to login?").
			Affirmative("Yes!").
			Negative("No.").
			Value(&confirms).
			WithTheme(huhTheme).
			Run()
		if err != nil {
			return nil, err
		}
		if !confirms {
			return nil, fmt.Errorf("aborting")
		}
		// no user auth, perform login flow
		t, err := performLoginFlow(ctx, state, authClient, tokenSource)
		if err != nil {
			return nil, fmt.Errorf("performing login: %v", err)
		}
		userToken = t
	} else {
		// check that the token is valid
		ll := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
		clOpts := connect.WithInterceptors(
			auth.Interceptors(ll, tokenSource)...,
		)
		userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)
		cerr := new(connect.Error)
		_, err := userClient.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
		if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
			// token isn't valid anymore, login again
			confirms := true
			err := huh.NewConfirm().
				Title("Your session has expired. Would you like to login again?").
				Affirmative("Yes!").
				Negative("No.").
				Value(&confirms).
				WithTheme(huhTheme).
				Run()
			if err != nil {
				return nil, err
			}
			if !confirms {
				return nil, fmt.Errorf("aborting")
			}
			t, err := performLoginFlow(ctx, state, authClient, tokenSource)
			if err != nil {
				return nil, fmt.Errorf("performing login: %v", err)
			}
			userToken = t
		} else if err != nil {
			return nil, fmt.Errorf("requesting whoami: %v", err)
		}
	}
	return userToken, nil
}

func performLoginFlow(
	ctx context.Context,
	state *state.State,
	authClient authv1connect.AuthServiceClient,
	tokenSource *auth.UserRefreshableTokenSource,
) (*typesv1.UserToken, error) {
	res, err := authClient.BeginDeviceAuth(ctx, connect.NewRequest(&authv1.BeginDeviceAuthRequest{}))
	if err != nil {
		return nil, fmt.Errorf("requesting auth URL: %v", err)
	}

	url := res.Msg.Url
	deviceCode := res.Msg.DeviceCode
	userCode := res.Msg.UserCode
	pollUntil := res.Msg.ExpiresAt
	pollInterval := res.Msg.PollInterval.AsDuration()
	loginfo("open your browser at URL %q", url)
	if err := browser.OpenURL(url); err != nil {
		return nil, fmt.Errorf("opening browser: %v", err)
	}

	ctx, cancel := context.WithDeadline(ctx, pollUntil.AsTime())
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var (
		userToken *typesv1.UserToken
		accountID int64
		machineID int64
	)
poll_for_tokens:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		res, err := authClient.CompleteDeviceAuth(ctx, connect.NewRequest(&authv1.CompleteDeviceAuthRequest{
			DeviceCode: deviceCode,
			UserCode:   userCode,

			ClaimAccountId:  state.AccountID,
			ClaimMachineId:  state.MachineID,
			Architecture:    runtime.GOARCH,
			OperatingSystem: runtime.GOOS,
		}))
		if err != nil {
			if cerr, ok := err.(*connect.Error); ok {
				switch cerr.Code() {
				case connect.CodeFailedPrecondition:
					continue poll_for_tokens
				}
			}
			return nil, fmt.Errorf("waiting for user to be authenticated: %v", err)
		}
		userToken = res.Msg.Token
		accountID = res.Msg.AccountId
		machineID = res.Msg.MachineId
		break poll_for_tokens

	}

	err = tokenSource.SetUserToken(ctx, userToken)
	if err != nil {
		return nil, fmt.Errorf("saving credentials to keyring: %v", err)
	}
	state.AccountID = &accountID
	state.MachineID = &machineID
	if err := state.WriteBack(); err != nil {
		return nil, fmt.Errorf("saving state")
	}
	return userToken, nil
}

func ensureOrgSelected(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
) (int64, error) {
	if state.CurrentOrgID != nil {
		return *state.CurrentOrgID, nil
	}
	clOpts := connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	)

	client := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)
	orgID, err := huhSelectOrganizations(ctx, client, "You belong to many orgs. Which one would you like to use?")
	if err != nil {
		return -1, err
	}
	state.CurrentOrgID = &orgID
	return orgID, state.WriteBack()
}

func huhSelectOrganizations(ctx context.Context, client userv1connect.UserServiceClient, title string) (int64, error) {
	var options []huh.Option[*typesv1.Organization]
	iter := ListOrganizations(ctx, client)
	for iter.Next() {
		org := iter.Current().Organization
		options = append(options, huh.NewOption(org.Name, org))
	}
	if err := iter.Err(); err != nil {
		return -1, fmt.Errorf("no org selected and couldn't list user orgs: %v", err)
	}
	if len(options) == 0 {
		return -1, fmt.Errorf("no org is attached to your user, this is a bug. please contact support at hi@humanlog.io")
	}
	if len(options) == 1 {
		return options[0].Value.Id, nil
	}
	var selected *typesv1.Organization
	err := huh.NewSelect[*typesv1.Organization]().
		Title(title).
		Options(options...).
		Value(&selected).
		WithTheme(huhTheme).
		Run()
	if err != nil {
		return -1, fmt.Errorf("prompting for org selection: %v", err)
	}
	return selected.Id, nil
}

func ensureAccountSelected(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
	orgID int64,
) (int64, error) {
	if state.CurrentAccountID != nil {
		return *state.CurrentAccountID, nil
	}
	clOpts := connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	)

	var options []huh.Option[*typesv1.Account]
	client := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
	iter := ListAccounts(ctx, orgID, client)
	for iter.Next() {
		item := iter.Current().Account
		options = append(options, huh.NewOption(item.Name, item))
	}
	if err := iter.Err(); err != nil {
		return -1, fmt.Errorf("no account selected and couldn't list user accounts: %v", err)
	}

	if len(options) == 0 {
		accountID, err := promptCreateAccount(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
		if err != nil {
			return -1, err
		}
		state.CurrentAccountID = &accountID
		return accountID, state.WriteBack()
	}
	if len(options) == 1 {
		state.CurrentAccountID = &options[0].Value.Id
		return *state.CurrentAccountID, state.WriteBack()
	}

	var (
		selected *typesv1.Account
	)
	err := huh.NewSelect[*typesv1.Account]().
		Title("You have access to multiple accounts. Which one would you like to use?").
		Options(options...).
		Value(&selected).
		WithTheme(huhTheme).
		Run()
	if err != nil {
		return -1, fmt.Errorf("prompting for account selection: %v", err)
	}

	state.CurrentOrgID = &selected.Id
	return *state.CurrentOrgID, state.WriteBack()
}

func ListOrganizations(ctx context.Context, client userv1connect.UserServiceClient) *iterapi.Iter[*userv1.ListOrganizationResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*userv1.ListOrganizationResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListOrganization(ctx, connect.NewRequest(&userv1.ListOrganizationRequest{
			Cursor: cursor,
			Limit:  limit,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func ListOrgUser(ctx context.Context, orgID int64, client organizationv1connect.OrganizationServiceClient) *iterapi.Iter[*organizationv1.ListUserResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*organizationv1.ListUserResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListUser(ctx, connect.NewRequest(&organizationv1.ListUserRequest{
			Cursor:         cursor,
			Limit:          limit,
			OrganizationId: orgID,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func ListAccounts(ctx context.Context, orgID int64, client organizationv1connect.OrganizationServiceClient) *iterapi.Iter[*organizationv1.ListAccountResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*organizationv1.ListAccountResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListAccount(ctx, connect.NewRequest(&organizationv1.ListAccountRequest{
			Cursor:         cursor,
			Limit:          limit,
			OrganizationId: orgID,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func ListAccountTokens(ctx context.Context, accountID int64, client tokenv1connect.TokenServiceClient) *iterapi.Iter[*tokenv1.ListAccountTokenResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*tokenv1.ListAccountTokenResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListAccountToken(ctx, connect.NewRequest(&tokenv1.ListAccountTokenRequest{
			Cursor:    cursor,
			Limit:     limit,
			AccountId: accountID,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func printFact(key string, fact any) {
	log.Printf(
		"- %s: %s",
		color.YellowString(key),
		color.CyanString(fmt.Sprintf("%v", fact)),
	)
}
