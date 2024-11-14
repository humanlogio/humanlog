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
	"github.com/blang/semver"
	"github.com/fatih/color"
	cliupdatepb "github.com/humanlogio/api/go/svc/cliupdate/v1"
	"github.com/humanlogio/api/go/svc/cliupdate/v1/cliupdatev1connect"
	types "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/selfupdate"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli"
)

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func shouldCheckForUpdate(cctx *cli.Context, cfg *config.Config, state *state.State) bool {
	if cctx.Args().First() == versionCmdName {
		return false // check is done already
	}
	if cfg.SkipCheckForUpdates != nil && *cfg.SkipCheckForUpdates {
		return false
	}
	return true
}

func shouldPromptAboutUpdate() bool {
	if !isTerminal(os.Stderr) || !isTerminal(os.Stdout) {
		// not in interactive mode
		return false
	}
	return true
}

func reqMeta(st *state.State) *types.ReqMeta {
	req := new(types.ReqMeta)
	if st == nil {
		return req
	}
	if st.MachineID != nil {
		req.MachineId = *st.MachineID
	}
	return req
}

func updateFromResMeta(st *state.State, res *types.ResMeta, latestKnownVersion *semver.Version, latestKnownVersionUpdatedAt *time.Time) error {
	changed := false
	if st.MachineID == nil || res.MachineId != *st.MachineID {
		st.MachineID = &res.MachineId
		changed = true
	}
	if st.LatestKnownVersion == nil && latestKnownVersion != nil {
		st.LatestKnownVersion = latestKnownVersion
		changed = true
	} else if st.LatestKnownVersion != nil && latestKnownVersion != nil && !st.LatestKnownVersion.EQ(*latestKnownVersion) {
		st.LatestKnownVersion = latestKnownVersion
		changed = true
	}
	if st.LastestKnownVersionUpdatedAt == nil && latestKnownVersionUpdatedAt != nil {
		st.LastestKnownVersionUpdatedAt = latestKnownVersionUpdatedAt
		changed = true
	} else if st.LastestKnownVersionUpdatedAt != nil && latestKnownVersionUpdatedAt != nil && !st.LastestKnownVersionUpdatedAt.Equal(*latestKnownVersionUpdatedAt) {
		st.LastestKnownVersionUpdatedAt = latestKnownVersionUpdatedAt
		changed = true
	}
	if !changed {
		return nil
	}
	return st.WriteBack()
}

const versionCmdName = "version"

func versionCmd(
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
		Name:  versionCmdName,
		Usage: "Interact with humanlog versions",
		Subcommands: cli.Commands{
			{
				Name:  "check",
				Usage: "checks whether a newer version is available",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					cfg := getCfg(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					httpClient := getHTTPClient(cctx, apiURL)
					var channelName *string
					if cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ReleaseChannel != nil {
						channelName = cfg.ExperimentalFeatures.ReleaseChannel
					}
					nextVersion, nextArtifact, hasUpdate, err := checkForUpdate(ctx, ll, cfg, state, apiURL, httpClient, tokenSource, channelName)
					if err != nil {
						return err
					}
					if !hasUpdate {
						log.Printf("you're already running the latest version: v%v", semverVersion.String())
						return nil
					}
					nextSV, err := nextVersion.AsSemver()
					if err != nil {
						return fmt.Errorf("invalid semver received: %w", err)
					}
					promptToUpdate(semverVersion, nextSV)
					log.Printf("- url: %s", nextArtifact.Url)
					log.Printf("- sha256: %s", nextArtifact.Sha256)
					log.Printf("- sig: %s", nextArtifact.Signature)
					return nil
				},
			},
			{
				Name:  "update",
				Usage: "self-update to the latest version",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					ll := getLogger(cctx)
					cfg := getCfg(cctx)
					state := getState(cctx)
					tokenSource := getTokenSource(cctx)
					apiURL := getAPIUrl(cctx)
					baseSiteURL := getBaseSiteURL(cctx)
					httpClient := getHTTPClient(cctx, apiURL)
					var channelName *string
					if cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ReleaseChannel != nil {
						channelName = cfg.ExperimentalFeatures.ReleaseChannel
					}
					_, _, hasUpdate, err := checkForUpdate(ctx, ll, cfg, state, apiURL, httpClient, tokenSource, channelName)
					if err != nil {
						return err
					}
					if !hasUpdate {
						log.Printf("you're already running the latest version: v%v", semverVersion.String())
						return nil
					}
					return selfupdate.UpgradeInPlace(ctx, baseSiteURL, channelName, os.Stdout, os.Stderr, os.Stdin)
				},
			},
		},
	}
}

type checkForUpdateRes struct {
	pb        *types.Version
	sem       semver.Version
	hasUpdate bool
}

func checkForUpdate(ctx context.Context, ll *slog.Logger, cfg *config.Config, state *state.State, apiURL string, httpClient *http.Client, tokenSource *auth.UserRefreshableTokenSource, channelName *string) (v *types.Version, a *types.VersionArtifact, hasUpdate bool, err error) {
	currentSV, err := version.AsSemver()
	if err != nil {
		return nil, nil, false, err
	}

	var clOpts []connect.ClientOption
	clOpts = append(clOpts, connect.WithInterceptors(auth.NewRefreshedUserAuthInterceptor(ll, tokenSource)))
	updateClient := cliupdatev1connect.NewUpdateServiceClient(httpClient, apiURL, clOpts...)
	res, err := updateClient.GetNextUpdate(ctx, connect.NewRequest(&cliupdatepb.GetNextUpdateRequest{
		ProjectName:            "humanlog",
		CurrentVersion:         version,
		MachineArchitecture:    runtime.GOARCH,
		MachineOperatingSystem: runtime.GOOS,
		Meta:                   reqMeta(state),
		ReleaseChannelName:     channelName,
	}))
	if err != nil {
		return nil, nil, false, err
	}
	msg := res.Msg

	lastCheckAt := time.Now()
	nextSV, err := msg.NextVersion.AsSemver()
	if err != nil {
		return nil, nil, false, err
	}
	if err := updateFromResMeta(state, msg.Meta, &nextSV, &lastCheckAt); err != nil {
		logwarn("failed to persist internal state: %v", err)
	}

	return msg.NextVersion, msg.NextArtifact, currentSV.LT(nextSV), nil
}

func asyncCheckForUpdate(ctx context.Context, ll *slog.Logger, cfg *config.Config, state *state.State, apiURL string, httpClient *http.Client, tokenSource *auth.UserRefreshableTokenSource, channelName *string) <-chan *checkForUpdateRes {
	out := make(chan *checkForUpdateRes, 1)
	go func() {
		defer close(out)
		nextVersion, _, hasUpdate, err := checkForUpdate(ctx, ll, cfg, state, apiURL, httpClient, tokenSource, channelName)
		if err != nil {
			if errors.Is(errors.Unwrap(err), context.Canceled) {
				return
			}
			// TODO: log to diagnostic file?
			logwarn("failed to check for update: %v", err)
			return
		}
		nexVersion, err := nextVersion.AsSemver()
		if err != nil {
			logwarn("next version is not a valid semver: %v", err)
			return
		}
		out <- &checkForUpdateRes{
			pb:        nextVersion,
			sem:       nexVersion,
			hasUpdate: hasUpdate,
		}
	}()
	return out
}

func promptToUpdate(from, to semver.Version) {
	log.Print(
		color.YellowString("Update available %s -> %s.", from, to),
	)
	log.Print(
		color.YellowString("Run `%s` to upgrade.", color.New(color.Bold).Sprint("humanlog version update")),
	)
}
