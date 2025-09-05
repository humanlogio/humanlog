package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
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
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {

	runsAsService := func(cfg *config.Config) bool {
		if cfg == nil {
			return false
		}
		expcfg := cfg.GetRuntime().GetExperimentalFeatures()
		if expcfg == nil {
			return false
		}
		if expcfg.ServeLocalhost != nil {
			return true
		}
		return false
	}

	ensureServiceEnabled := func(cctx *cli.Context) error {
		ctx := getCtx(cctx)
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
		loginfo("uninstalling service if it existed")
		if err := svc.Uninstall(); err != nil {
			logdebug("failed to uninstall service (was it installed?): %v", err)
		} else {
			loginfo("uninstalled service")
		}
		loginfo("installing humanlog service")
		if err := svc.Install(); err != nil {
			return fmt.Errorf("can't install service: %v", err)
		}
		loginfo("service installed")
		if os.Getenv("INSIDE_HUMANLOG_SELF_UPDATE") == "" {
			// we're not self-updating, so we need to restart the service

			loginfo("stopping service if it was running")
			if err = svc.Stop(ctx); err != nil {
				logwarn("failed to stop: %v", err)
			}
			loginfo("starting service")
			if err := svc.Start(ctx); err != nil {
				return fmt.Errorf("failed to start service: %v", err)
			}
		}
		return nil
	}

	var (
		forceNonInteractiveFlag = cli.BoolFlag{Name: "force-non-interactive"}
	)

	return cli.Command{
		Name:   onboardingCmdName,
		Usage:  "Onboarding humanlog after installs or updates",
		Hidden: true,
		Flags:  []cli.Flag{forceNonInteractiveFlag},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			cfg := getCfg(cctx)
			state := getState(cctx)
			ll := getLogger(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			baseURL, err := url.Parse(getBaseSiteURL(cctx))
			if err != nil {
				return fmt.Errorf("compilation error, invalid base site URL built into binary: %v", err)
			}
			userToken, err := tokenSource.GetUserToken(ctx)
			if err != nil {
				return fmt.Errorf("looking up user credentials: %v", err)
			}

			httpClient := getHTTPClient(cctx, apiURL)
			clOpts := getConnectOpts(cctx)
			// clOpts := connect.WithClientOptions(connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...))
			// userSvc := userv1connect.NewUserServiceClient(httpClient, apiURL, clOpts...)

			logdebug("checking logged in status")
			user, err := checkUserLoggedIn(ctx, ll, httpClient, apiURL, tokenSource, clOpts)
			if err != nil {
				logwarn("unable to check if you're logged in: %v", err)
			}

			defer func() {
				logdebug("checking if should run humanlog as a service")
				if !runsAsService(cfg) {
					logdebug("humanlog should not run as a servive")
					return
				}
				logdebug("humanlog should run as a servive, enabling it (due to config)")
				if err := ensureServiceEnabled(cctx); err != nil {
					logerror("unable to configure humanlog service: %v", err)
				} else {
					logdebug("humanlog service is configured")
				}
			}()

			if !isTerminal(os.Stdout) || cctx.Bool(forceNonInteractiveFlag.Name) {
				logdebug("stdout isn't a terminal, disabling interactive prompts")
				in := `# humanlog updates

Hey there!

Thanks for installing this version of humanlog. If this is your first time around, try this out:

` + "```bash" + `
humanlog onboarding
` + "```" + `

This will help you get started and learn everything that humanlog has to offer.

Bye! <3`

				out, err := glamour.Render(in, "dark")
				if err != nil {
					return err
				}
				fmt.Print(out)

				return nil
			}

			expcfg := cfg.GetRuntime().GetExperimentalFeatures()

			if err := setReleaseChannel(cfg); err != nil {
				logdebug("can't write back config to set release channel: %v", err)
			}

			now := time.Now()
			askAgainAfter := 24 * time.Hour

			// by default:
			// - we always turn on the query engine
			// - we always want the user to signup if they're not signed up and
			var (
				hasQueryEngine           = (expcfg != nil && expcfg.ServeLocalhost != nil)
				isSignedUp               = (userToken != nil)
				isLoggedIn               = (user != nil)
				isSignedUpButNotLoggedIn = isSignedUp && !isLoggedIn
				wantsQueryEngine         = !hasQueryEngine
				wantsSignup              = !isSignedUp
				askedAboutQueryEngine    = state != nil && state.LastPromptedToEnableLocalhostAt != nil
				askedAboutSignup         = state != nil && state.LastPromptedToSignupAt != nil

				username string
			)

			if userToken != nil {
				username = userToken.Username
			}

			// we only prompt about the query engine if a user previously refused it.
			// new installs get the query engine by default
			promptQueryEngine := false
			if askedAboutQueryEngine {
				// we assume no until told otherwise
				wantsQueryEngine = false
				if wasMoreThanTimeAgo(now, state.LastPromptedToEnableLocalhostAt, askAgainAfter) {
					logdebug("prompting for query-engine because it's been a while since asking")
					promptQueryEngine = true
				} else {
					// we asked recently and they apparently refused, so wait later for another update
					logdebug("not prompting for query-engine because we asked too recently and were denied")
					promptQueryEngine = false
				}
			}
			// we only prompt about signup if a user previously refused it.
			// new installs sign up by default
			promptSignup := false
			if askedAboutSignup {
				// we assume no until told otherwise
				wantsSignup = false
				if wasMoreThanTimeAgo(now, state.LastPromptedToSignupAt, askAgainAfter) {
					logdebug("prompting for signup because it's been a while since asking")
					promptSignup = true
				} else {
					logdebug("not prompting for signup because we asked too recently and were denied")
					promptSignup = false
				}
			}

			var fields []huh.Field
			if promptQueryEngine {
				logdebug("prompting about query engine")
				var titleSignupExtra, titleDescriptionExtra string
				if wantsSignup {
					titleSignupExtra = "\nAnd since you are not logged in, this will also prompt you to log in.\n"
					titleDescriptionExtra = " and signin"
				}
				fields = append(fields,
					huh.NewConfirm().
						Title("Humanlog now includes a log query engine, right here in your pocket.\n\n"+
							"You can use it to query your logs, plot graphs and do general log observability stuff. All on your machine!\n\n"+
							"To enable this feature, humanlog needs to run a background service.\n"+titleSignupExtra).
						Description("Do you want to enable the log query engine"+titleDescriptionExtra+"?").
						Affirmative("Yes!").Negative("No.").
						Value(&wantsQueryEngine),
				)
				state.LastPromptedToEnableLocalhostAt = ptr(time.Now())
			} else {
				logdebug("not prompting about query engine")
			}
			if promptSignup && !promptQueryEngine {
				logdebug("prompting about signing up")
				fields = append(fields,
					huh.NewConfirm().
						Title("New features are coming soon. Sign in to learn more.").
						Description("Sign up to learn about upcoming releases?").
						Affirmative("Yes!").Negative("No").Value(&wantsSignup),
				)
				state.LastPromptedToSignupAt = ptr(time.Now())
			} else {
				logdebug("not prompting about signing up")
			}

			if username == "" {
				fields = append(fields,
					huh.NewInput().
						Title("Select a username").
						Description("It should match the regexp `^[a-zA-Z0-9][a-zA-Z0-9-]+$`").
						Value(&username),
				)
			}

			if len(fields) > 0 {
				err := huh.NewForm(huh.NewGroup(fields...)).WithTheme(huhTheme).Run()
				if err != nil {
					return err
				}
				if err := state.WriteBack(); err != nil {
					logwarn("failed to record your answer: %v", err)
				}
			}

			if wantsQueryEngine {
				if expcfg == nil {
					expcfg = &typesv1.RuntimeConfig_ExperimentalFeatures{}
				}
				serveLocalhost, err := config.GetDefaultLocalhostConfig()
				if err != nil {
					logerror("getting default value for localhost log engine config: %v", err)
				} else {
					expcfg.ServeLocalhost = serveLocalhost
					if err := cfg.WriteBack(); err != nil {
						logerror("failed to update config file: %v", err)
					}
				}
			}

			if wantsSignup || isSignedUpButNotLoggedIn {
				authClient := authv1connect.NewAuthServiceClient(httpClient, apiURL)

				var redirectURL string
				if isSignedUpButNotLoggedIn {
					redirectURL = baseURL.JoinPath("/cli/login/success").String()
				} else {
					redirectURL = baseURL.JoinPath("/onboarding").String()
				}

				_, err := performLoginFlow(ctx, state, authClient, tokenSource, username, 0, redirectURL)
				if err != nil {
					logerror("failed to sign up or sign in: %v", err)
				}
			}

			loginfo("keep an eye on `%s` for more updates!", baseURL.String())

			return nil
		},
	}
}
func setReleaseChannel(cfg *config.Config) error {
	expcfg := cfg.GetRuntime().GetExperimentalFeatures()
	if expcfg == nil {
		return nil
	}
	releaseChannel := os.Getenv("HUMANLOG_CHANNEL")
	if releaseChannel == "" {
		return nil
	}
	if releaseChannel == defaultReleaseChannel {
		return nil
	}

	if expcfg.ReleaseChannel != nil && *expcfg.ReleaseChannel == releaseChannel {
		return nil
	}
	expcfg.ReleaseChannel = ptr(releaseChannel)
	return cfg.WriteBack()
}

func wasMoreThanTimeAgo(now time.Time, t *time.Time, ago time.Duration) bool {
	if t == nil {
		return true
	}
	lastAgo := now.Sub(*t)
	return lastAgo > ago
}

func pjson(v any) string {
	out, err := json.MarshalIndent(v, "", "   ")
	if err != nil {
		panic(err)
	}
	return string(out)
}
