package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const (
	machineCmdName = "machine"
)

func machineCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	return cli.Command{
		Hidden: hideUnreleasedFeatures == "true",
		Name:   machineCmdName,
		Usage:  "Manage machines in the current environment.",
		Before: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)
			clOpts := getConnectOpts(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
			if err != nil {
				return err
			}
			return nil
		},
		Subcommands: []cli.Command{
			{
				Name:  "register",
				Usage: "register this machine to save logs in an environment",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)
					clOpts := getConnectOpts(cctx)
					environmentToken, err := createIngestionToken(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, clOpts)
					if err != nil {
						return fmt.Errorf("ingestion token couldn't be generated: %v", err)
					}
					state.IngestionToken = environmentToken
					if err := state.WriteBack(); err != nil {
						return fmt.Errorf("writing back generated ingestion token: %v", err)
					}
					return nil
				},
			},
			{
				Name:  "deregister",
				Usage: "deregister this machine from saving logs in an environment",
				Action: func(cctx *cli.Context) error {
					state := getState(cctx)
					state.IngestionToken = nil
					if err := state.WriteBack(); err != nil {
						return fmt.Errorf("writing back generated ingestion token: %v", err)
					}
					return nil
				},
			},
		},
	}
}
