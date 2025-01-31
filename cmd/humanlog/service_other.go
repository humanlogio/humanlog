//go:build !darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/urfave/cli"
)

const (
	serviceCmdName = "service"
)

func serviceCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
) cli.Command {

	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      serviceCmdName,
		ShortName: "svc",
		Usage:     "not supported on this platform",
		Action: func(cctx *cli.Context) error {
			return fmt.Errorf("no supported on this platform")
		},
	}
}
