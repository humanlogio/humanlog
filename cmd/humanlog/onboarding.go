package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/charmbracelet/huh"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const onboardingCmdName = "onboarding"

func onboardingCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(*cli.Context, string) *http.Client,
) cli.Command {
	return cli.Command{
		Name:   onboardingCmdName,
		Usage:  "Onboarding humanlog after installs or updates",
		Hidden: true,
		Action: func(cctx *cli.Context) error {
			wantsSignup := true
			err := huh.NewConfirm().Title("Welcome to humanlog. New features are coming up soon!").
				Description("Would you like to sign-up to learn more?").
				Affirmative("Yes!").
				Negative("No.").
				Value(&wantsSignup).
				WithAccessible(accessibleTUI).
				WithTheme(huhTheme).Run()
			if err != nil {
				return err
			}
			if wantsSignup {
				loginfo("awesome, thanks for your interest!")
				ctx := getCtx(cctx)
				state := getState(cctx)
				tokenSource := getTokenSource(cctx)
				apiURL := getAPIUrl(cctx)
				httpClient := getHTTPClient(cctx, apiURL)
				authClient := authv1connect.NewAuthServiceClient(httpClient, apiURL)
				_, err := performLoginFlow(ctx, state, authClient, tokenSource)
				return err
			}
			loginfo("sounds good, enjoy humanlog! keep an eye on `https://humanlog.io` if you want to learn more")
			return nil
		},
	}
}
