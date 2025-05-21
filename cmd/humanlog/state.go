package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const (
	stateCmdName = "state"
)

func stateCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
) cli.Command {

	return cli.Command{
		Name:   stateCmdName,
		Hidden: hideUnreleasedFeatures == "true",
		Usage:  "Manipulate humanlog's state.",
		Subcommands: []cli.Command{
			{
				Name: "hack",
				Subcommands: []cli.Command{
					{
						Name: "ensure-exists",
						Action: func(cctx *cli.Context) error {
							state := getState(cctx)
							loginfo("ensuring state file exists")
							return state.WriteBack()
						},
					},
					{
						Name: "record-last-prompted-to-enable-localhost-now",
						Action: func(cctx *cli.Context) error {
							state := getState(cctx)
							state.LastPromptedToEnableLocalhostAt = ptr(time.Now())
							loginfo("recording LastPromptedToEnableLocalhostAt=%q", state.LastPromptedToEnableLocalhostAt.String())
							return state.WriteBack()
						},
					},
					{
						Name: "record-last-prompted-to-enable-localhost-lastyear",
						Action: func(cctx *cli.Context) error {
							state := getState(cctx)
							state.LastPromptedToEnableLocalhostAt = ptr(time.Now().AddDate(-1, 0, 0))
							loginfo("recording LastPromptedToEnableLocalhostAt=%q", state.LastPromptedToEnableLocalhostAt.String())
							return state.WriteBack()
						},
					},
					{
						Name: "record-last-prompted-to-signup-now",
						Action: func(cctx *cli.Context) error {
							state := getState(cctx)
							state.LastPromptedToSignupAt = ptr(time.Now())
							loginfo("recording LastPromptedToSignupAt=%q", state.LastPromptedToSignupAt.String())
							return state.WriteBack()
						},
					},
					{
						Name: "record-last-prompted-to-signup-lastyear",
						Action: func(cctx *cli.Context) error {
							state := getState(cctx)
							state.LastPromptedToSignupAt = ptr(time.Now().AddDate(-1, 0, 0))
							loginfo("recording LastPromptedToSignupAt=%q", state.LastPromptedToSignupAt.String())
							return state.WriteBack()
						},
					},
				},
			},
		},
	}
}
