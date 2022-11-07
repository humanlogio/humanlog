package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"

	"github.com/aybabtme/rgbterm"
	"github.com/blang/semver"
	"github.com/bufbuild/connect-go"
	cliupdatepb "github.com/humanlog-io/api/go/svc/cliupdate/v1"
	"github.com/humanlog-io/api/go/svc/cliupdate/v1/cliupdatev1connect"
	types "github.com/humanlog-io/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/sink/stdiosink"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/mattn/go-colorable"
	"github.com/urfave/cli"
)

var (
	Version       = &types.Version{Minor: 6}
	semverVersion = func() semver.Version {
		v, err := Version.AsSemver()
		if err != nil {
			panic(err)
		}
		return v
	}()
)

func fatalf(c *cli.Context, format string, args ...interface{}) {
	log.Printf(format, args...)
	cli.ShowAppHelp(c)
	os.Exit(1)
}

func main() {
	app := newApp()

	prefix := rgbterm.FgString(app.Name+"> ", 99, 99, 99)

	log.SetOutput(colorable.NewColorableStderr())
	log.SetFlags(0)
	log.SetPrefix(prefix)
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
func newApp() *cli.App {

	configFlag := cli.StringFlag{
		Name:  "config",
		Usage: "specify a config file to use, otherwise uses the default one",
	}

	skip := cli.StringSlice{}
	keep := cli.StringSlice{}

	skipFlag := cli.StringSliceFlag{
		Name:  "skip",
		Usage: "keys to skip when parsing a log entry",
		Value: &skip,
	}

	keepFlag := cli.StringSliceFlag{
		Name:  "keep",
		Usage: "keys to keep when parsing a log entry",
		Value: &keep,
	}

	sortLongest := cli.BoolTFlag{
		Name:  "sort-longest",
		Usage: "sort by longest key after having sorted lexicographically",
	}

	skipUnchanged := cli.BoolTFlag{
		Name:  "skip-unchanged",
		Usage: "skip keys that have the same value than the previous entry",
	}

	truncates := cli.BoolFlag{
		Name:  "truncate",
		Usage: "truncates values that are longer than --truncate-length",
	}

	truncateLength := cli.IntFlag{
		Name:  "truncate-length",
		Usage: "truncate values that are longer than this length",
		Value: *config.DefaultConfig.TruncateLength,
	}

	colorFlag := cli.StringFlag{
		Name:  "color",
		Usage: "specify color mode: auto, on/force, off",
		Value: stdiosink.DefaultStdioOpts.ColorFlag,
	}

	lightBg := cli.BoolFlag{
		Name:  "light-bg",
		Usage: "use black as the base foreground color (for terminals with light backgrounds)",
	}

	timeFormat := cli.StringFlag{
		Name:  "time-format",
		Usage: "output time format, see https://golang.org/pkg/time/ for details",
		Value: stdiosink.DefaultStdioOpts.TimeFormat,
	}

	ignoreInterrupts := cli.BoolFlag{
		Name:  "ignore-interrupts, i",
		Usage: "ignore interrupts",
	}

	messageFields := cli.StringSlice{}
	messageFieldsFlag := cli.StringSliceFlag{
		Name:   "message-fields, m",
		Usage:  "Custom JSON fields to search for the log message. (i.e. mssge, data.body.message)",
		EnvVar: "HUMANLOG_MESSAGE_FIELDS",
		Value:  &messageFields,
	}

	timeFields := cli.StringSlice{}
	timeFieldsFlag := cli.StringSliceFlag{
		Name:   "time-fields, t",
		Usage:  "Custom JSON fields to search for the log time. (i.e. logtime, data.body.datetime)",
		EnvVar: "HUMANLOG_TIME_FIELDS",
		Value:  &timeFields,
	}

	levelFields := cli.StringSlice{}
	levelFieldsFlag := cli.StringSliceFlag{
		Name:   "level-fields, l",
		Usage:  "Custom JSON fields to search for the log level. (i.e. somelevel, data.level)",
		EnvVar: "HUMANLOG_LEVEL_FIELDS",
		Value:  &levelFields,
	}

	app := cli.NewApp()
	app.Author = "Antoine Grondin"
	app.Email = "antoinegrondin@gmail.com"
	app.Name = "humanlog"
	app.Version = semverVersion.String()
	app.Usage = "reads structured logs from stdin, makes them pretty on stdout!"

	var (
		ctx           context.Context
		cancel        context.CancelFunc
		cfg           *config.Config
		stateFilepath string
		statefile     *state.State
		updateRes     <-chan *checkForUpdateRes
	)

	app.Before = func(c *cli.Context) error {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)

		configFilepath, err := config.GetDefaultConfigFilepath()
		if err != nil {
			return fmt.Errorf("looking up config file path: %v", err)
		}
		// read config
		if c.IsSet(configFlag.Name) {
			configFilepath = c.String(configFlag.Name)
			cfgFromFlag, err := config.ReadConfigFile(configFilepath, &config.DefaultConfig)
			if err != nil {
				return fmt.Errorf("reading --config file %q: %v", configFilepath, err)
			}
			cfg = cfgFromFlag
		} else {
			cfgFromDir, err := config.ReadConfigFile(configFilepath, &config.DefaultConfig)
			if err != nil {
				return fmt.Errorf("reading default config file: %v", err)
			}
			cfg = cfgFromDir
		}

		stateFilepath, err = state.GetDefaultStateFilepath()
		if err != nil {
			return fmt.Errorf("looking up state file path: %v", err)
		}
		// read state
		statefile, err = state.ReadStateFile(stateFilepath, &state.DefaultState)
		if err != nil {
			return fmt.Errorf("reading default config file: %v", err)
		}

		if cfg.CheckForUpdates != nil && *cfg.CheckForUpdates {
			req := &checkForUpdateReq{
				arch:    runtime.GOARCH,
				os:      runtime.GOOS,
				current: Version,
			}
			if statefile != nil {
				if statefile.AccountID != nil {
					req.accountID = *statefile.AccountID
				}
				if statefile.MachineID != nil {
					req.machineID = *statefile.MachineID
				}
			}
			updateRes = checkForUpdate(ctx, req)
		}
		return nil
	}
	app.After = func(c *cli.Context) error {
		cancel()
		select {
		case res := <-updateRes:
			if semverVersion.LT(res.sem) {
				log.Printf("a new version of humanlog is available: please update")
			}
			updateStatefile := false
			if statefile.AccountID == nil && res.accountID > 0 {
				updateStatefile = true
				statefile.AccountID = &res.accountID
			}
			if statefile.MachineID == nil && res.machineID > 0 {
				updateStatefile = true
				statefile.MachineID = &res.machineID
			}
			if updateStatefile {
				if err := state.WriteStateFile(stateFilepath, statefile); err != nil {
					log.Printf("failed to update statefile")
				}
			}
		default:
		}
		return nil
	}

	app.Flags = []cli.Flag{configFlag, skipFlag, keepFlag, sortLongest, skipUnchanged, truncates, truncateLength, colorFlag, lightBg, timeFormat, ignoreInterrupts, messageFieldsFlag, timeFieldsFlag, levelFieldsFlag}
	app.Action = func(c *cli.Context) error {
		// flags overwrite config file
		if c.IsSet(sortLongest.Name) {
			cfg.SortLongest = ptr(c.BoolT(sortLongest.Name))
		}
		if c.IsSet(skipUnchanged.Name) {
			cfg.SkipUnchanged = ptr(c.BoolT(skipUnchanged.Name))
		}
		if c.IsSet(truncates.Name) {
			cfg.Truncates = ptr(c.BoolT(truncates.Name))
		}
		if c.IsSet(truncateLength.Name) {
			cfg.TruncateLength = ptr(c.Int(truncateLength.Name))
		}
		if c.IsSet(lightBg.Name) {
			cfg.LightBg = ptr(c.Bool(lightBg.Name))
		}
		if c.IsSet(timeFormat.Name) {
			cfg.TimeFormat = ptr(c.String(timeFormat.Name))
		}
		if c.IsSet(colorFlag.Name) {
			cfg.ColorMode = ptr(c.String(colorFlag.Name))
		}
		if c.IsSet(skipFlag.Name) {
			cfg.Skip = ptr([]string(skip))
		}
		if c.IsSet(keepFlag.Name) {
			cfg.Keep = ptr([]string(keep))
		}
		if c.IsSet(messageFieldsFlag.Name) {
			cfg.MessageFields = ptr([]string(messageFields))
		}

		if c.IsSet(timeFieldsFlag.Name) {
			cfg.TimeFields = ptr([]string(timeFields))
		}

		if c.IsSet(levelFieldsFlag.Name) {
			cfg.LevelFields = ptr([]string(levelFields))
		}

		if c.IsSet(ignoreInterrupts.Name) {
			cfg.Interrupt = ptr(c.Bool(ignoreInterrupts.Name))
		}

		// apply the config
		if *cfg.Interrupt {
			signal.Ignore(os.Interrupt)
		}

		if len(*cfg.Skip) > 0 && len(*cfg.Keep) > 0 {
			fatalf(c, "can only use one of %q and %q", skipFlag.Name, keepFlag.Name)
		}

		sinkOpts, errs := stdiosink.StdioOptsFrom(*cfg)
		if len(errs) > 0 {
			for _, err := range errs {
				log.Printf("config error: %v", err)
			}
		}
		sink := stdiosink.NewStdio(colorable.NewColorableStdout(), sinkOpts)
		handlerOpts := humanlog.HandlerOptionsFrom(*cfg)

		log.Print("reading stdin...")
		if err := humanlog.Scanner(ctx, os.Stdin, sink, handlerOpts); err != nil {
			log.Fatalf("scanning caught an error: %v", err)
		}

		return nil
	}
	return app
}

func ptr[T any](v T) *T {
	return &v
}

const apiURL = "https://api.humanlog.io"

type checkForUpdateReq struct {
	arch      string
	os        string
	accountID int64
	machineID int64
	current   *types.Version
}
type checkForUpdateRes struct {
	pb        *types.Version
	sem       semver.Version
	url       string
	sha256    string
	accountID int64
	machineID int64
}

func checkForUpdate(ctx context.Context, req *checkForUpdateReq) <-chan *checkForUpdateRes {
	out := make(chan *checkForUpdateRes, 1)
	go func() {
		defer close(out)
		client := &http.Client{}
		updateClient := cliupdatev1connect.NewUpdateServiceClient(client, apiURL)
		res, err := updateClient.GetNextUpdate(ctx, &connect.Request[cliupdatepb.GetNextUpdateRequest]{
			Msg: &cliupdatepb.GetNextUpdateRequest{
				CurrentVersion:         Version,
				AccountId:              req.accountID,
				MachineId:              req.machineID,
				MachineArchitecture:    req.arch,
				MachineOperatingSystem: req.os,
			},
		})
		if err != nil {
			log.Printf("looking for update failed: %v", err)
			return
		}
		nextVersion := res.Msg.NextVersion

		nexVersion, err := nextVersion.AsSemver()
		if err != nil {
			log.Printf("looking for update returned bogus version: %v", err)
			return
		}
		if nexVersion.EQ(semverVersion) {
			log.Printf("running latest version: %v", semverVersion)
		} else if nexVersion.LT(semverVersion) {
			log.Printf("you appear to be running an unreleased version")
		} else if nexVersion.GT(semverVersion) {
			log.Printf("next version is %q, you're running %q", nexVersion, semverVersion)
		}
		out <- &checkForUpdateRes{
			pb:        nextVersion,
			sem:       nexVersion,
			machineID: res.Msg.Machine.Id,
		}
	}()
	return out
}
