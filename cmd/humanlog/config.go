package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"connectrpc.com/connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
)

const (
	configCmdName = "config"
)

func configCmd(
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
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      configCmdName,
		ShortName: "cfg",
		Usage:     "Manipulate humanlog's configuration.",
		Subcommands: []cli.Command{
			{
				Name: "reset-to-defaults",
				Action: func(cctx *cli.Context) error {
					fp, err := config.GetDefaultConfigFilepath()
					if err != nil {
						return fmt.Errorf("getting default config filepath: %v", err)
					}
					cfg, err := config.GetDefaultConfig(defaultReleaseChannel)
					if err != nil {
						return fmt.Errorf("preparing default config: %v", err)
					}
					if err := config.WriteConfigFile(fp, cfg); err != nil {
						return fmt.Errorf("writing default config to filepath: %v", err)
					}
					loginfo("reset config to defaults: %v", fp)
					return nil
				},
			},
			{
				Name: "edit",
				Action: func(cctx *cli.Context) error {
					baseSiteU, err := url.Parse(getBaseSiteURL(cctx))
					if err != nil {
						return fmt.Errorf("parsing base site URL: %v", err)
					}
					editConfigPath := baseSiteU.JoinPath("/localhost/edit").String()
					return browser.OpenURL(editConfigPath)
				},
			},
			{
				Name: "show",
				Action: func(cctx *cli.Context) error {
					cfg := getCfg(cctx)
					out, err := json.MarshalIndent(cfg, "", "   ")
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(out)
					return err
				},
			},
			{
				Name: "show-defaults",
				Action: func(cctx *cli.Context) error {
					cfg, err := config.GetDefaultConfig(defaultReleaseChannel)
					if err != nil {
						return err
					}

					out, err := json.MarshalIndent(cfg, "", "   ")
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(out)
					return err
				},
			},
			{
				Name: "hack",
				Subcommands: []cli.Command{
					{
						Name:        "for-netskope",
						Description: "hacks to make netskope happy: http2 -> http1",
						Action: func(cctx *cli.Context) error {
							cfg := getCfg(cctx)
							if cfg.Runtime == nil {
								cfg.Runtime = &typesv1.RuntimeConfig{}
							}
							if cfg.Runtime.ApiClient == nil {
								cfg.Runtime.ApiClient = &typesv1.RuntimeConfig_ClientConfig{}
							}
							httpProtocol := typesv1.RuntimeConfig_ClientConfig_HTTP1
							cfg.Runtime.ApiClient.HttpProtocol = &httpProtocol
							return cfg.WriteBack()
						},
					},
				},
			},
		},
	}
}
