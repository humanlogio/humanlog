package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/99designs/keyring"
	"github.com/aybabtme/rgbterm"
	"github.com/blang/semver"
	types "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
	"github.com/humanlogio/humanlog/pkg/sink/teesink"
	"github.com/mattn/go-colorable"
	"github.com/urfave/cli"
	"golang.org/x/net/http2"
)

var (
	versionMajor      = "0"
	versionMinor      = "0"
	versionPatch      = "0"
	versionPrerelease = "devel"
	versionBuild      = ""
	version           = func() *types.Version {
		var prerelease []string
		if versionPrerelease != "" {
			prerelease = append(prerelease, versionPrerelease)
		}
		return &types.Version{
			Major:       int32(mustatoi(versionMajor)),
			Minor:       int32(mustatoi(versionMinor)),
			Patch:       int32(mustatoi(versionPatch)),
			Prereleases: prerelease,
			Build:       versionBuild,
		}
	}()
	semverVersion = func() semver.Version {
		v, err := version.AsSemver()
		if err != nil {
			panic(err)
		}
		return v
	}()
	defaultApiAddr = "https://api.humanlog.io"
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

	apiServerAddr := cli.StringFlag{
		Name:   "api",
		Value:  defaultApiAddr,
		Usage:  "address of the api server",
		EnvVar: "HUMANLOG_API_URL",
		Hidden: true,
	}

	app := cli.NewApp()
	app.Author = "humanlog.io"
	app.Email = "hi@humanlog.io"
	app.Name = "humanlog"
	app.Version = semverVersion.String()
	app.Usage = "reads structured logs from stdin, makes them pretty on stdout!"
	app.Description = `humanlog parses logs and makes them easier to read and search.

   When invoked with no argument, it consumes stdin and parses it,
   attempts to make it prettier on stdout. It also allows searching
   the logs that were parsed, both in a TUI by pressing "s" or in a
   webapp by pressing "space".

   If registered to ingest logs via "humanlog machine register" logs
   will be saved to humanlog.io for vizualization, searching and
   analysis.
`

	var (
		ctx        context.Context
		cancel     context.CancelFunc
		cfg        *config.Config
		statefile  *state.State
		httpClient = &http.Client{
			Transport: &http2.Transport{},
		}
		localhostHttpClient = &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		}
		promptedToUpdate *semver.Version
		updateRes        <-chan *checkForUpdateRes
		apiURL           = ""
		keyringName      = "humanlog"

		getCtx     = func(*cli.Context) context.Context { return ctx }
		getLogger  = func(*cli.Context) *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stderr, nil)) }
		getCfg     = func(*cli.Context) *config.Config { return cfg }
		getState   = func(*cli.Context) *state.State { return statefile }
		getKeyring = func(cctx *cli.Context) (keyring.Keyring, error) {
			stateDir, err := state.GetDefaultStateDirpath()
			if err != nil {
				return nil, err
			}
			return keyring.Open(keyring.Config{
				ServiceName:            keyringName,
				KeychainSynchronizable: true,
				FileDir:                stateDir,
			})

		}
		getTokenSource = func(cctx *cli.Context) *auth.UserRefreshableTokenSource {
			return auth.NewRefreshableTokenSource(func() (keyring.Keyring, error) {
				return getKeyring(cctx)
			})
		}
		getAPIUrl     = func(*cli.Context) string { logdebug("using api at %q", apiURL); return apiURL }
		getHTTPClient = func(cctx *cli.Context) *http.Client {
			u, _ := url.Parse(apiURL)
			if host, _, _ := net.SplitHostPort(u.Host); host == "localhost" {
				getLogger(cctx).Debug("using localhost client")
				return localhostHttpClient
			}
			return httpClient
		}
		getLocalhostHTTPClient = func(*cli.Context) *http.Client {
			return localhostHttpClient
		}
	)

	app.Before = func(c *cli.Context) error {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)

		// read config
		if c.IsSet(configFlag.Name) {
			configFilepath := c.String(configFlag.Name)
			cfgFromFlag, err := config.ReadConfigFile(configFilepath, &config.DefaultConfig)
			if err != nil {
				return fmt.Errorf("reading --config file %q: %v", configFilepath, err)
			}
			cfg = cfgFromFlag
		} else {
			configFilepath, err := config.GetDefaultConfigFilepath()
			if err != nil {
				return fmt.Errorf("looking up config file path: %v", err)
			}
			cfgFromDir, err := config.ReadConfigFile(configFilepath, &config.DefaultConfig)
			if err != nil {
				return fmt.Errorf("reading default config file: %v", err)
			}
			cfg = cfgFromDir
		}
		if c.String(apiServerAddr.Name) != "" {
			apiURL = c.String(apiServerAddr.Name)
			getLogger(c).Debug("contacting api at %q (due to flag")
		}

		stateFilepath, err := state.GetDefaultStateFilepath()
		if err != nil {
			return fmt.Errorf("looking up state file path: %v", err)
		}
		// read state
		statefile, err = state.ReadStateFile(stateFilepath, &state.DefaultState)
		if err != nil {
			return fmt.Errorf("reading default config file: %v", err)
		}

		if shouldCheckForUpdate(c, cfg, statefile) {
			if statefile.LatestKnownVersion != nil && statefile.LatestKnownVersion.GT(semverVersion) {
				promptedToUpdate = statefile.LatestKnownVersion
				if shouldPromptAboutUpdate() {
					promptToUpdate(semverVersion, *statefile.LatestKnownVersion)
				}
			}
			ll := getLogger(c)
			tokenSource := getTokenSource(c)
			updateRes = asyncCheckForUpdate(ctx, ll, cfg, statefile, apiURL, httpClient, tokenSource)
		}

		return nil
	}
	app.After = func(c *cli.Context) error {
		cancel()
		select {
		case res, ok := <-updateRes:
			if !ok {
				return nil
			}
			if res.hasUpdate {
				alreadyPromptedForSameUpdate := promptedToUpdate != nil && promptedToUpdate.GTE(res.sem)
				if !alreadyPromptedForSameUpdate {
					if shouldPromptAboutUpdate() {
						promptToUpdate(semverVersion, res.sem)
					}
				}
			}
		default:
		}
		return nil
	}
	app.Commands = append(
		app.Commands,
		versionCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		authCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		organizationCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		accountCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		machineCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		queryCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient),
		gennyCmd(getCtx, getLogger, getCfg, getState),
	)
	app.Flags = []cli.Flag{configFlag, skipFlag, keepFlag, sortLongest, skipUnchanged, truncates, truncateLength, colorFlag, lightBg, timeFormat, ignoreInterrupts, messageFieldsFlag, timeFieldsFlag, levelFieldsFlag, apiServerAddr}
	app.Action = func(cctx *cli.Context) error {
		// flags overwrite config file
		if cctx.IsSet(sortLongest.Name) {
			cfg.SortLongest = ptr(cctx.BoolT(sortLongest.Name))
		}
		if cctx.IsSet(skipUnchanged.Name) {
			cfg.SkipUnchanged = ptr(cctx.BoolT(skipUnchanged.Name))
		}
		if cctx.IsSet(truncates.Name) {
			cfg.Truncates = ptr(cctx.BoolT(truncates.Name))
		}
		if cctx.IsSet(truncateLength.Name) {
			cfg.TruncateLength = ptr(cctx.Int(truncateLength.Name))
		}
		if cctx.IsSet(lightBg.Name) {
			cfg.LightBg = ptr(cctx.Bool(lightBg.Name))
		}
		if cctx.IsSet(timeFormat.Name) {
			cfg.TimeFormat = ptr(cctx.String(timeFormat.Name))
		}
		if cctx.IsSet(colorFlag.Name) {
			cfg.ColorMode = ptr(cctx.String(colorFlag.Name))
		}
		if cctx.IsSet(skipFlag.Name) {
			cfg.Skip = ptr([]string(skip))
		}
		if cctx.IsSet(keepFlag.Name) {
			cfg.Keep = ptr([]string(keep))
		}
		if cctx.IsSet(strings.Split(messageFieldsFlag.Name, ",")[0]) {
			cfg.MessageFields = ptr([]string(messageFields))
		}

		if cctx.IsSet(strings.Split(timeFieldsFlag.Name, ",")[0]) {
			cfg.TimeFields = ptr([]string(timeFields))
		}

		if cctx.IsSet(strings.Split(levelFieldsFlag.Name, ",")[0]) {
			cfg.LevelFields = ptr([]string(levelFields))
		}

		if cctx.IsSet(strings.Split(ignoreInterrupts.Name, ",")[0]) {
			cfg.Interrupt = ptr(cctx.Bool(strings.Split(ignoreInterrupts.Name, ",")[0]))
		}

		// apply the config
		if *cfg.Interrupt {
			signal.Ignore(os.Interrupt)
		}

		if cfg.Skip != nil && cfg.Keep != nil {
			if len(*cfg.Skip) > 0 && len(*cfg.Keep) > 0 {
				fatalf(cctx, "can only use one of %q and %q", skipFlag.Name, keepFlag.Name)
			}
		}

		sinkOpts, errs := stdiosink.StdioOptsFrom(*cfg)
		if len(errs) > 0 {
			for _, err := range errs {
				logerror("config error: %v", err)
			}
		}
		var sink sink.Sink
		sink = stdiosink.NewStdio(colorable.NewColorableStdout(), sinkOpts)
		handlerOpts := humanlog.HandlerOptionsFrom(*cfg)

		if cfg.ExperimentalFeatures != nil {
			if cfg.ExperimentalFeatures.SendLogsToCloud != nil && *cfg.ExperimentalFeatures.SendLogsToCloud {
				// TODO(antoine): remove this codepath, it's redundant with the localhost port path
				ll := getLogger(cctx)
				apiURL := getAPIUrl(cctx)
				remotesink, err := ingest(ctx, ll, cctx, apiURL, getCfg, getState, getTokenSource, getHTTPClient)
				if err != nil {
					return fmt.Errorf("can't send logs: %v", err)
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
					defer cancel()
					if err := remotesink.Flush(ctx); err != nil {
						ll.ErrorContext(ctx, "couldn't flush buffered log", slog.Any("err", err))
					} else {
						ll.InfoContext(ctx, "done sending all logs")
					}
				}()
				loginfo("saving to %s", apiURL)
				sink = teesink.NewTeeSink(sink, remotesink)
			}

			if cfg.ExperimentalFeatures.ServeLocalhostOnPort != nil {
				port := *cfg.ExperimentalFeatures.ServeLocalhostOnPort
				state := getState(cctx)
				// TODO(antoine): all logs to a single location, right now there's code logging
				// randomly everywhere
				ll := getLogger(cctx)
				var machineID uint64
				for state.MachineID == nil {
					// no machine ID assigned, ensure machine gets onboarded via the loggin flow
					// TODO(antoine): if an account token exists, auto-onboard the machine. it's probably
					// not an interactive session
					_, err := ensureLoggedIn(ctx, cctx, state, getTokenSource(cctx), apiURL, getHTTPClient(cctx))
					if err != nil {
						return fmt.Errorf("this feature requires a valid machine ID, which requires an account. failed to login: %v", err)
					}
				}
				machineID = uint64(*state.MachineID)
				localhostSink, done, err := startLocalhostServer(ctx, ll, cfg, state, machineID, port, getLocalhostHTTPClient(cctx), version)
				if err != nil {
					loginfo("starting experimental localhost service: %v", err)
				} else {
					sink = teesink.NewTeeSink(sink, localhostSink)
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
					defer cancel()
					if err := done(ctx); err != nil {
						ll.ErrorContext(ctx, "couldn't flush buffered log (localhost)", slog.Any("err", err))
					} else {
						ll.InfoContext(ctx, "done sending all logs")
					}
				}()
			}
		}

		loginfo("reading stdin...")
		if err := humanlog.Scan(ctx, os.Stdin, sink, handlerOpts); err != nil {
			logerror("scanning caught an error: %v", err)
		}

		return nil
	}
	return app
}

func ptr[T any](v T) *T {
	return &v
}

func mustatoi(a string) int {
	i, err := strconv.Atoi(a)
	if err != nil {
		panic(err)
	}
	return i
}
