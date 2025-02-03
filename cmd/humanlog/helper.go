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
	userpb "github.com/humanlogio/api/go/svc/user/v1"
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
		user, err := checkUserLoggedIn(ctx, ll, httpClient, apiURL, tokenSource)
		if err != nil {
			return nil, fmt.Errorf("requesting whoami: %v", err)
		}
		if user == nil {
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
		}
	}
	return userToken, nil
}

func checkUserLoggedIn(ctx context.Context, ll *slog.Logger, httpClient *http.Client, apiURL string, tokenSource *auth.UserRefreshableTokenSource) (*typesv1.User, error) {
	clOpts := connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	)
	cerr := new(connect.Error)
	userClient := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts)
	res, err := userClient.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
	if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return res.Msg.User, nil
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
		break poll_for_tokens

	}

	err = tokenSource.SetUserToken(ctx, userToken)
	if err != nil {
		return nil, fmt.Errorf("saving credentials to keyring: %v", err)
	}
	state.MachineID = &machineID
	if err := state.WriteBack(); err != nil {
		return nil, fmt.Errorf("saving state")
	}
	return userToken, nil
}

func performLogoutFlow(ctx context.Context, userSvc userv1connect.UserServiceClient, tokenSource *auth.UserRefreshableTokenSource) error {
	res, err := userSvc.GetLogoutURL(ctx, connect.NewRequest(&userpb.GetLogoutURLRequest{}))
	if err != nil {
		return fmt.Errorf("retrieving logout URL")
	}
	if err := browser.OpenURL(res.Msg.GetLogoutUrl()); err != nil {
		return fmt.Errorf("opening logout URL")
	}
	return tokenSource.ClearToken(ctx)
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

func ensureEnvironmentSelected(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
	orgID int64,
) (int64, error) {
	if state.CurrentEnvironmentID != nil {
		return *state.CurrentEnvironmentID, nil
	}
	clOpts := connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	)

	var options []huh.Option[*typesv1.Environment]
	client := organizationv1connect.NewOrganizationServiceClient(httpClient, apiURL, clOpts)
	iter := ListEnvironments(ctx, client)
	for iter.Next() {
		item := iter.Current().Environment
		options = append(options, huh.NewOption(item.Name, item))
	}
	if err := iter.Err(); err != nil {
		return -1, fmt.Errorf("no environment selected and couldn't list user environments: %v", err)
	}

	if len(options) == 0 {
		environmentID, err := promptCreateEnvironment(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, orgID)
		if err != nil {
			return -1, err
		}
		state.CurrentEnvironmentID = &environmentID
		return environmentID, state.WriteBack()
	}
	if len(options) == 1 {
		state.CurrentEnvironmentID = &options[0].Value.Id
		return *state.CurrentEnvironmentID, state.WriteBack()
	}

	var (
		selected *typesv1.Environment
	)
	err := huh.NewSelect[*typesv1.Environment]().
		Title("You have access to multiple environments. Which one would you like to use?").
		Options(options...).
		Value(&selected).
		WithTheme(huhTheme).
		Run()
	if err != nil {
		return -1, fmt.Errorf("prompting for environment selection: %v", err)
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

func ListOrgUser(ctx context.Context, client organizationv1connect.OrganizationServiceClient) *iterapi.Iter[*organizationv1.ListUserResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*organizationv1.ListUserResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListUser(ctx, connect.NewRequest(&organizationv1.ListUserRequest{
			Cursor: cursor,
			Limit:  limit,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func ListEnvironments(ctx context.Context, client organizationv1connect.OrganizationServiceClient) *iterapi.Iter[*organizationv1.ListEnvironmentResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*organizationv1.ListEnvironmentResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListEnvironment(ctx, connect.NewRequest(&organizationv1.ListEnvironmentRequest{
			Cursor: cursor,
			Limit:  limit,
		}))
		if err != nil {
			return nil, nil, err
		}
		return list.Msg.Items, list.Msg.Next, nil
	})
}

func ListEnvironmentTokens(ctx context.Context, environmentID int64, client tokenv1connect.TokenServiceClient) *iterapi.Iter[*tokenv1.ListEnvironmentTokenResponse_ListItem] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*tokenv1.ListEnvironmentTokenResponse_ListItem, *typesv1.Cursor, error) {
		list, err := client.ListEnvironmentToken(ctx, connect.NewRequest(&tokenv1.ListEnvironmentTokenRequest{
			Cursor:        cursor,
			Limit:         limit,
			EnvironmentId: environmentID,
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
