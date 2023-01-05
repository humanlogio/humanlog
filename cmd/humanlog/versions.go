package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/blang/semver"
	"github.com/bufbuild/connect-go"
	"github.com/fatih/color"
	cliupdatepb "github.com/humanlogio/api/go/svc/cliupdate/v1"
	"github.com/humanlogio/api/go/svc/cliupdate/v1/cliupdatev1connect"
	types "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/selfupdate"
	"github.com/humanlogio/humanlog/internal/pkg/state"
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
	if !isTerminal(os.Stderr) || !isTerminal(os.Stdout) {
		// not in interactive mode
		return false
	}
	if cfg.SkipCheckForUpdates != nil && *cfg.SkipCheckForUpdates {
		return false
	}
	return true
}

var httpClient = &http.Client{
	Transport: &http.Transport{},
}

func reqMeta(st *state.State) *types.ReqMeta {
	req := new(types.ReqMeta)
	if st == nil {
		return req
	}
	if st.AccountID != nil {
		req.AccountId = *st.AccountID
	}
	if st.MachineID != nil {
		req.MachineId = *st.MachineID
	}
	return req
}

func updateFromResMeta(st *state.State, res *types.ResMeta, lastUpdateCheck *time.Time) error {
	changed := false
	if st.AccountID == nil || res.AccountId != *st.AccountID {
		st.AccountID = &res.AccountId
		changed = true
	}
	if st.MachineID == nil || res.MachineId != *st.MachineID {
		st.MachineID = &res.MachineId
		changed = true
	}
	if st.LastUpdateCheckAt == nil && lastUpdateCheck != nil {
		st.LastUpdateCheckAt = lastUpdateCheck
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
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
) cli.Command {
	return cli.Command{
		Name: versionCmdName,
		Subcommands: cli.Commands{
			{
				Name: "check",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					cfg := getCfg(cctx)
					state := getState(cctx)
					nextVersion, nextArtifact, hasUpdate, err := checkForUpdate(ctx, cfg, state)
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
					log.Print(
						color.YellowString("Update available %s -> %s.", semverVersion, nextSV),
					)
					log.Print(
						color.YellowString("Run %s to upgrade.", color.New(color.Bold).Sprint("humanlog version update")),
					)
					log.Printf("- url: %s", nextArtifact.Url)
					log.Printf("- sha256: %s", nextArtifact.Sha256)
					log.Printf("- sig: %s", nextArtifact.Signature)
					return nil
				},
			},
			{
				Name: "update",
				Action: func(cctx *cli.Context) error {
					ctx := getCtx(cctx)
					cfg := getCfg(cctx)
					state := getState(cctx)
					_, _, hasUpdate, err := checkForUpdate(ctx, cfg, state)
					if err != nil {
						return err
					}
					if !hasUpdate {
						log.Printf("you're already running the latest version: v%v", semverVersion.String())
						return nil
					}
					return selfupdate.UpgradeInPlace(ctx, os.Stdout, os.Stderr, os.Stdin)
				},
			},
		},
	}
}

const apiURL = "https://api.humanlog.io"

type checkForUpdateReq struct {
	arch    string
	os      string
	state   *state.State
	current *types.Version
}
type checkForUpdateRes struct {
	pb        *types.Version
	sem       semver.Version
	hasUpdate bool
}

func checkForUpdate(ctx context.Context, cfg *config.Config, state *state.State) (v *types.Version, a *types.VersionArtifact, hasUpdate bool, err error) {
	currentSV, err := version.AsSemver()
	if err != nil {
		return nil, nil, false, err
	}

	updateClient := cliupdatev1connect.NewUpdateServiceClient(httpClient, apiURL)
	res, err := updateClient.GetNextUpdate(ctx, connect.NewRequest(&cliupdatepb.GetNextUpdateRequest{
		ProjectName:            "apictl", // "humanlog",
		CurrentVersion:         version,
		MachineArchitecture:    runtime.GOARCH,
		MachineOperatingSystem: runtime.GOOS,
		Meta:                   reqMeta(state),
	}))
	if err != nil {
		return nil, nil, false, err
	}
	msg := res.Msg

	lastCheckAt := time.Now()
	if err := updateFromResMeta(state, msg.Meta, &lastCheckAt); err != nil {
		log.Printf("failed to persist internal state: %v", err)
	}

	nextSV, err := msg.NextVersion.AsSemver()
	if err != nil {
		return nil, nil, false, err
	}
	return msg.NextVersion, msg.NextArtifact, currentSV.LT(nextSV), nil
}

func asyncCheckForUpdate(ctx context.Context, req *checkForUpdateReq, cfg *config.Config, state *state.State) <-chan *checkForUpdateRes {
	out := make(chan *checkForUpdateRes, 1)
	go func() {
		defer close(out)
		nextVersion, _, hasUpdate, err := checkForUpdate(ctx, cfg, state)
		if err != nil {
			// TODO: log to diagnostic file
			log.Printf("failed to check for update")
			return
		}
		nexVersion, err := nextVersion.AsSemver()
		if err != nil {
			log.Printf("next version is not a valid semver: %v", err)
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
