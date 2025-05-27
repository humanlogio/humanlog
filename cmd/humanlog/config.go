package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

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
					editConfigPath := baseSiteU.JoinPath("/settings/localhost").String()
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
				Name: "set",
				Action: func(cctx *cli.Context) error {
					cfg := getCfg(cctx)
					for _, directive := range cctx.Args() {
						if err := applySetDirective(cfg, directive); err != nil {
							return fmt.Errorf("applying directive %q: %v", directive, err)
						}
					}
					return cfg.WriteBack()
				},
			},
			{
				Name: "enable",
				Subcommands: []cli.Command{
					{
						Name:        "query-engine",
						Usage:       "(experimental) enables the localhost query engine",
						Description: "(experimental) enables the localhost query engine",
						Action: func(cctx *cli.Context) error {
							ctx := getCtx(cctx)
							cfg := getCfg(cctx)
							if cfg.Runtime == nil {
								cfg.Runtime = &typesv1.RuntimeConfig{}
							}
							if cfg.Runtime.ExperimentalFeatures == nil {
								cfg.Runtime.ExperimentalFeatures = &typesv1.RuntimeConfig_ExperimentalFeatures{}
							}
							localhostCfg, err := config.GetDefaultLocalhostConfig()
							if err != nil {
								return fmt.Errorf("getting default localhost config: %v", err)
							}
							cfg.Runtime.ExperimentalFeatures.ServeLocalhost = localhostCfg
							if err := cfg.WriteBack(); err != nil {
								return fmt.Errorf("enabling localhost feature: %v", err)
							}
							svc, err := prepareServiceCmd(cctx,
								getCtx,
								getLogger,
								getCfg,
								getState,
								getTokenSource,
								getAPIUrl,
								getBaseSiteURL,
								getHTTPClient,
							)
							if err != nil {
								return fmt.Errorf("failed to get humanlog service details: %v", err)
							}
							// in case it already ran
							if err = svc.Stop(ctx); err != nil {
								logdebug("failed to stop if already started: %v", err)
							}
							if err := svc.Uninstall(); err != nil {
								logdebug("failed to uninstall service if already installed: %v", err)
							}
							if err := svc.Install(); err != nil {
								return fmt.Errorf("installing humanlog service: %v", err)
							}
							if err := svc.Start(ctx); err != nil {
								return fmt.Errorf("installing humanlog service: %v", err)
							}
							loginfo("localhost query engine enabled")
							return nil
						},
					},
				},
			},
			{
				Name: "disable",
				Subcommands: []cli.Command{
					{
						Name:        "query-engine",
						Usage:       "(experimental) disables the localhost query engine",
						Description: "(experimental) disables the localhost query engine",
						Action: func(cctx *cli.Context) error {
							ctx := getCtx(cctx)
							cfg := getCfg(cctx)

							svc, err := prepareServiceCmd(cctx,
								getCtx,
								getLogger,
								getCfg,
								getState,
								getTokenSource,
								getAPIUrl,
								getBaseSiteURL,
								getHTTPClient,
							)
							if err != nil {
								return fmt.Errorf("failed to get humanlog service details: %v", err)
							}

							// in case it already ran
							if err = svc.Stop(ctx); err != nil {
								logdebug("failed to stop if already started: %v", err)
							}
							if err := svc.Uninstall(); err != nil {
								logdebug("failed to uninstall service if already installed: %v", err)
							}

							if cfg.Runtime != nil && cfg.Runtime.ExperimentalFeatures != nil && cfg.Runtime.ExperimentalFeatures.ServeLocalhost != nil {
								cfg.Runtime.ExperimentalFeatures.ServeLocalhost = nil
								if err := cfg.WriteBack(); err != nil {
									return fmt.Errorf("enabling localhost feature: %v", err)
								}
							}

							loginfo("localhost query engine disabled")
							return nil
						},
					},
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

func applySetDirective(cfg *config.Config, directive string) error {
	pathElements, value, err := parseSetDirective(directive)
	if err != nil {
		return fmt.Errorf("parsing directive: %v", err)
	}
	if err := setValue(cfg, pathElements, value); err != nil {
		return fmt.Errorf("applying directive %q: %v", directive, err)
	}
	return nil
}

func parseSetDirective(directive string) (pathElements []string, value any, err error) {
	path, valueStr, found := strings.Cut(directive, "=")
	if !found {
		return nil, value, fmt.Errorf("no `=` found in directive")
	}
	if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
		return nil, value, fmt.Errorf("parsing value in directive (%q): %v", valueStr, err)
	}
	pathElements = strings.Split(path, ".")

	return pathElements, value, nil
}

func setValue(cfg *config.Config, pathElements []string, value any) error {
	buf, err := json.Marshal(cfg.CurrentConfig)
	if err != nil {
		return err
	}
	mutatable := make(map[string]any)
	if err := json.Unmarshal(buf, &mutatable); err != nil {
		return err
	}

	pos := mutatable
	for i, el := range pathElements {
		nextPos, ok := pos[el]
		if !ok {
			nextPos = make(map[string]any)
			pos[el] = nextPos
		}
		if i == len(pathElements)-1 {
			pos[el] = value
			break
		}
		nextTypedPos, ok := nextPos.(map[string]any)
		if !ok {
			pathSoFar := strings.Join(pathElements[:i], ".")
			return fmt.Errorf("invalid path, not indexable (not an object, but a %T): %v", pos[el], pathSoFar)
		}
		pos = nextTypedPos
	}

	newBuf, err := json.Marshal(mutatable)
	if err != nil {
		return err
	}

	return json.Unmarshal(newBuf, cfg.CurrentConfig)
}
