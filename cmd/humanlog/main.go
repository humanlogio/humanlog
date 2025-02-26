package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/99designs/keyring"
	"github.com/aybabtme/rgbterm"
	"github.com/blang/semver"
	"github.com/charmbracelet/huh"
	"github.com/gen2brain/beeep"
	types "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
	"github.com/humanlogio/humanlog/pkg/sink/teesink"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
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
			for _, pre := range strings.Split(versionPrerelease, ".") {
				if pre != "" {
					prerelease = append(prerelease, pre)
				}
			}
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
	defaultApiAddr         = "https://api.humanlog.io"
	defaultBaseSiteAddr    = "https://humanlog.io"
	defaultReleaseChannel  = "main"
	hideUnreleasedFeatures = ""

	huhTheme = func() *huh.Theme {
		base := huh.ThemeCatppuccin()
		base.Focused.FocusedButton = base.Focused.FocusedButton.Bold(true).Underline(true)
		base.Focused.BlurredButton = base.Focused.BlurredButton.Bold(false).Underline(false).Strikethrough(true)
		base.Blurred.FocusedButton = base.Focused.FocusedButton.Bold(true).Underline(true)
		base.Blurred.BlurredButton = base.Focused.BlurredButton.Bold(false).Underline(false).Strikethrough(true)
		return base
	}()
)

func fatalf(c *cli.Context, format string, args ...interface{}) {
	log.Printf(format, args...)
	if err := cli.ShowAppHelp(c); err != nil {
		panic(err)
	}
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
		Value: 15,
	}

	colorFlag := cli.StringFlag{
		Name:  "color",
		Usage: "specify color mode: auto, on/force, off",
		Value: "auto",
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
	baseSiteServerAddr := cli.StringFlag{
		Name:   "basesite",
		Value:  defaultBaseSiteAddr,
		Usage:  "address of the base site server",
		EnvVar: "HUMANLOG_BASE_SITE_URL",
		Hidden: true,
	}

	debug := cli.BoolFlag{
		Name:   "debug",
		EnvVar: "HUMANLOG_DEBUG",
		Hidden: true,
	}

	useHTTP1 := cli.BoolFlag{
		Name:   "use-http1",
		EnvVar: "HUMANLOG_USE_HTTP1",
		Hidden: true,
	}
	useProtocol := cli.StringFlag{
		Name:   "use-protocol",
		EnvVar: "HUMANLOG_USE_PROTOCOL",
		Hidden: true,
	}

	app := cli.NewApp()
	app.Author = "humanlog.io"
	app.Email = "antoine@webscale.lol"
	app.Name = "humanlog"
	app.Version = semverVersion.String()
	app.Usage = "reads structured logs from stdin, makes them pretty on stdout!"
	app.Description = `humanlog parses logs and makes them easier to read and search.

   When invoked with no argument, it consumes stdin and parses it,
   attempting to make detected logs prettier on stdout.`
	if hideUnreleasedFeatures != "true" {
		app.Description += `
   It also allows searching
   the logs that were parsed, both in a TUI by pressing "s" or in a
   webapp by pressing "space".

   If registered to ingest logs via "humanlog machine register" logs
   will be saved to humanlog.io for vizualization, searching and
   analysis.
`
	}

	var (
		ctx             context.Context
		cancel          context.CancelFunc
		cfg             *config.Config
		statefile       *state.State
		dialer                            = &net.Dialer{Timeout: time.Second}
		tlsClientConfig                   = &tls.Config{}
		httpTransport   http.RoundTripper = &http2.Transport{
			TLSClientConfig: tlsClientConfig,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return tls.DialWithDialer(dialer, network, addr, cfg)
			},
		}
		httpClient = &http.Client{
			Transport: httpTransport,
		}
		localhostHttpTransport = &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
		localhostHttpClient = &http.Client{
			Transport: localhostHttpTransport,
		}
		clOpts           []connect.ClientOption
		promptedToUpdate *semver.Version
		updateRes        <-chan *checkForUpdateRes
		apiURL           = ""
		baseSiteURL      = ""
		keyringName      = "humanlog"

		getCtx    = func(*cli.Context) context.Context { return ctx }
		getLogger = func(*cli.Context) *slog.Logger {
			return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				AddSource: true,
				Level:     slogLevel(),
			}))
		}
		getCfg     = func(*cli.Context) *config.Config { return cfg }
		getState   = func(*cli.Context) *state.State { return statefile }
		getKeyring = func(*cli.Context) (keyring.Keyring, error) {
			stateDir, err := state.GetDefaultStateDirpath()
			if err != nil {
				return nil, err
			}
			return keyring.Open(keyring.Config{
				AllowedBackends: []keyring.BackendType{keyring.FileBackend},
				ServiceName:     keyringName,
				FileDir:         stateDir,
				FilePasswordFunc: func(s string) (pwd string, err error) {
					return "", nil
				},
			})
		}
		getTokenSource = func(cctx *cli.Context) *auth.UserRefreshableTokenSource {
			return auth.NewRefreshableTokenSource(func() (keyring.Keyring, error) {
				return getKeyring(cctx)
			})
		}
		getAPIUrl = func(*cli.Context) string {
			if apiURL == "" {
				apiURL = defaultApiAddr
			}
			logdebug("using api at %q", apiURL)
			return apiURL
		}
		getBaseSiteURL = func(*cli.Context) string {
			if baseSiteURL == "" {
				baseSiteURL = defaultBaseSiteAddr
			}
			logdebug("using basesite at %q", baseSiteURL)
			return baseSiteURL
		}
		getHTTPClient = func(cctx *cli.Context, apiURL string) *http.Client {
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
		getConnectOpts = func(cctx *cli.Context) []connect.ClientOption {
			return clOpts
		}
	)
	var closers []func()
	app.Before = func(c *cli.Context) error {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)

		// read config
		dfltCfg, err := config.GetDefaultConfig(defaultReleaseChannel)
		if err != nil {
			panic(err)
		}

		if c.IsSet(configFlag.Name) {
			configFilepath := c.String(configFlag.Name)
			cfgFromFlag, err := config.ReadConfigFile(configFilepath, dfltCfg, false)
			if err != nil {
				return fmt.Errorf("reading --config file %q: %v", configFilepath, err)
			}
			cfg = cfgFromFlag
		} else {
			configFilepath, err := config.GetDefaultConfigFilepath()
			if err != nil {
				return fmt.Errorf("looking up config file path: %v", err)
			}
			cfgFromDir, err := config.ReadConfigFile(configFilepath, dfltCfg, true)
			if err != nil {
				return fmt.Errorf("reading default config file: %v", err)
			}
			cfg = cfgFromDir
		}
		if c.String(apiServerAddr.Name) != "" {
			apiURL = c.String(apiServerAddr.Name)
			logdebug("api URL set to %q (due to --%s flag or $%s env var)", apiURL, apiServerAddr.Name, apiServerAddr.EnvVar)
		}
		if c.String(baseSiteServerAddr.Name) != "" {
			baseSiteURL = c.String(baseSiteServerAddr.Name)
			logdebug("base site URL set to %q (due to --%s flag or $%s env var)", baseSiteURL, baseSiteServerAddr.Name, baseSiteServerAddr.EnvVar)
		}

		if c.IsSet(useHTTP1.Name) && c.Bool(useHTTP1.Name) {
			httpTransport = &http.Transport{TLSClientConfig: tlsClientConfig}
			httpClient.Transport = httpTransport
			logdebug("using http/1 client instead of http/2")
		} else {
			protocol := cfg.GetRuntime().GetApiClient().GetHttpProtocol()
			switch protocol {
			case types.RuntimeConfig_ClientConfig_HTTP2:
				// no change
			case types.RuntimeConfig_ClientConfig_HTTP1:
				httpTransport = &http.Transport{TLSClientConfig: tlsClientConfig}
				httpClient.Transport = httpTransport
				logdebug("using http/1 client instead of http/2")
			default:
				return fmt.Errorf("unexpected HTTP protocol: %#v", protocol)
			}
		}

		if c.IsSet(useProtocol.Name) {
			protocol := c.String(useProtocol.Name)
			switch protocol {
			case "grpc":
				clOpts = append(clOpts, connect.WithGRPC())
			case "grpc-web":
				clOpts = append(clOpts, connect.WithGRPCWeb())
			case "protojson":
				clOpts = append(clOpts, connect.WithProtoJSON())
			default:
				return fmt.Errorf("unknown protocol (must be one of %v): %q", []string{"grpc", "grpc-web", "protojson"}, protocol)
			}
		} else {
			protocol := cfg.GetRuntime().GetApiClient().GetRpcProtocol()
			switch protocol {
			case types.RuntimeConfig_ClientConfig_GRPC:
				clOpts = append(clOpts, connect.WithGRPC())
			case types.RuntimeConfig_ClientConfig_GRPC_WEB:
				clOpts = append(clOpts, connect.WithGRPCWeb())
			case types.RuntimeConfig_ClientConfig_PROTOJSON:
				clOpts = append(clOpts, connect.WithProtoJSON())
			default:
				return fmt.Errorf("unexpected RPC protocol: %#v", protocol)
			}
		}

		if sslKeylogFile := os.Getenv("SSLKEYLOGFILE"); sslKeylogFile != "" {
			if !c.Bool(debug.Name) {
				return fmt.Errorf("flag --%q is required to use SSLKEYLOGFILE", debug.Name)
			} else {
				logwarn("saving TLS secrets to SSLKEYLOGFILE=%q", sslKeylogFile)
				keylogFile, err := os.Create(sslKeylogFile)
				if err != nil {
					return fmt.Errorf("creating SSLKEYLOGFILE file %q: %v", sslKeylogFile, err)
				}
				closers = append(closers, func() {
					if err := keylogFile.Close(); err != nil {
						logerror("failed to close TLS secret SSLKEYLOGFILE=%q: %v", sslKeylogFile, err)
					} else {
						logwarn("saved TLS secrets to SSLKEYLOGFILE=%q, please delete it when you're done debugging", sslKeylogFile)
					}
				})
				tlsClientConfig.KeyLogWriter = keylogFile
			}
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
			clOpts := getConnectOpts(c)
			var channelName *string
			expcfg := cfg.GetRuntime().GetExperimentalFeatures()
			if expcfg != nil && expcfg.ReleaseChannel != nil {
				channelName = expcfg.ReleaseChannel
			}
			updateRes = asyncCheckForUpdate(ctx, ll, cfg, statefile, apiURL, httpClient, tokenSource, channelName, clOpts)
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
		for _, closer := range closers {
			closer()
		}
		return nil
	}
	app.Commands = append(
		app.Commands,
		onboardingCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		versionCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		authCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		serviceCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		configCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		organizationCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		environmentCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		machineCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		queryCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		gennyCmd(getCtx, getLogger, getCfg, getState),
	)
	app.Flags = []cli.Flag{configFlag, skipFlag, keepFlag, sortLongest, skipUnchanged, truncates, truncateLength, colorFlag, timeFormat, ignoreInterrupts, messageFieldsFlag, timeFieldsFlag, levelFieldsFlag, apiServerAddr, baseSiteServerAddr, debug, useHTTP1, useProtocol}
	app.Action = func(cctx *cli.Context) error {
		// flags overwrite config file
		if cfg.CurrentConfig == nil {
			cfg.CurrentConfig = &types.LocalhostConfig{}
		}
		if cfg.Formatter == nil {
			cfg.Formatter = &types.FormatConfig{}
		}
		if cfg.Parser == nil {
			cfg.Parser = &types.ParseConfig{}
		}
		if cfg.Runtime == nil {
			cfg.Runtime = &types.RuntimeConfig{}
		}
		if cctx.IsSet(sortLongest.Name) {
			cfg.Formatter.SortLongest = ptr(cctx.BoolT(sortLongest.Name))
		}
		if cctx.IsSet(skipUnchanged.Name) {
			cfg.Formatter.SkipUnchanged = ptr(cctx.BoolT(skipUnchanged.Name))
		}
		if cctx.IsSet(truncates.Name) {
			if cctx.Bool(truncates.Name) && cfg.Formatter.Truncation == nil {
				cfg.Formatter.Truncation = &types.FormatConfig_Truncation{}
			}
			if !cctx.Bool(truncates.Name) && cfg.Formatter.Truncation != nil {
				cfg.Formatter.Truncation = nil
			}
		}
		if cctx.IsSet(truncateLength.Name) {
			if cfg.Formatter.Truncation != nil {
				cfg.Formatter.Truncation.Length = cctx.Int64(truncateLength.Name)
			}
		}
		if cctx.IsSet(timeFormat.Name) {
			if cfg.Formatter.Time == nil {
				cfg.Formatter.Time = &types.FormatConfig_Time{}
			}
			cfg.Formatter.Time.Format = ptr(cctx.String(timeFormat.Name))
		}
		if cctx.IsSet(colorFlag.Name) {
			cm, err := config.ParseColorMode(cctx.String(colorFlag.Name))
			if err != nil {
				return err
			}
			cfg.Formatter.TerminalColorMode = &cm
		}
		if cctx.IsSet(skipFlag.Name) {
			cfg.Formatter.SkipFields = []string(skip)
		}
		if cctx.IsSet(keepFlag.Name) {
			cfg.Formatter.KeepFields = []string(keep)
		}
		if cctx.IsSet(strings.Split(messageFieldsFlag.Name, ",")[0]) {
			if cfg.Parser.Message == nil {
				cfg.Parser.Message = &types.ParseConfig_Message{}
			}
			cfg.Parser.Message.FieldNames = []string(messageFields)
		}
		if cctx.IsSet(strings.Split(timeFieldsFlag.Name, ",")[0]) {
			if cfg.Parser.Timestamp == nil {
				cfg.Parser.Timestamp = &types.ParseConfig_Time{}
			}
			cfg.Parser.Timestamp.FieldNames = []string(timeFields)
		}
		if cctx.IsSet(strings.Split(levelFieldsFlag.Name, ",")[0]) {
			if cfg.Parser.Level == nil {
				cfg.Parser.Level = &types.ParseConfig_Level{}
			}
			cfg.Parser.Level.FieldNames = []string(levelFields)
		}

		if cctx.IsSet(strings.Split(ignoreInterrupts.Name, ",")[0]) {
			cfg.Runtime.Interrupt = ptr(cctx.Bool(strings.Split(ignoreInterrupts.Name, ",")[0]))
		}

		// apply the config
		if cfg.Runtime.Interrupt != nil && *cfg.Runtime.Interrupt {
			signal.Ignore(os.Interrupt)
		}

		if len(cfg.Formatter.SkipFields) > 0 && len(cfg.Formatter.KeepFields) > 0 {
			fatalf(cctx, "can only use one of %q and %q", skipFlag.Name, keepFlag.Name)
		}

		sinkOpts, errs := stdiosink.StdioOptsFrom(cfg.Formatter)
		if len(errs) > 0 {
			for _, err := range errs {
				logerror("config error: %v", err)
			}
		}
		var (
			sink sink.Sink
			err  error
		)
		sink, err = stdiosink.NewStdio(colorable.NewColorableStdout(), sinkOpts)
		if err != nil {
			return fmt.Errorf("preparing stdio printer: %v", err)
		}
		handlerOpts := humanlog.HandlerOptionsFrom(cfg.Parser)

		rtcfg := cfg.Runtime
		if rtcfg != nil && rtcfg.ExperimentalFeatures != nil {
			expcfg := rtcfg.ExperimentalFeatures
			if expcfg.SendLogsToCloud != nil && *expcfg.SendLogsToCloud {
				ll := getLogger(cctx)
				apiURL := getAPIUrl(cctx)
				notifyUnableToIngest := func(err error) {
					// TODO: notify using system notification?
					logerror("configured to ingest, but unable to do so: %v", err)
					msg := "Your logs are not being sent!"
					var cerr *connect.Error
					if errors.As(err, &cerr) {
						if cerr.Code() == connect.CodeResourceExhausted {
							msg += "\n\n- " + cerr.Message()
						} else {
							msg += "\n\n- " + cerr.Error()
						}
					} else {
						msg += "\n\n" + "An unexpected error occured while trying to ingest your logs, see your terminal for details."
						logerror("err=%T", err)
					}

					if err := beeep.Alert("humanlog has problems!", msg, ""); err != nil {
						logerror("couldn't send desktop notification: %v", err)
						if err := beeep.Beep(3000, 1); err != nil {
							logerror("can't even beeep :'( -> %w", err)
						}
						os.Exit(1)
					}
				}

				flushTimeout := 300 * time.Millisecond
				ingestctx, ingestcancel := context.WithCancel(context.WithoutCancel(ctx))
				go func() {
					<-ctx.Done()
					time.Sleep(2 * flushTimeout) // give it 2x timeout to flush before nipping the ctx entirely
					ingestcancel()
				}()
				remotesink, err := ingest(ingestctx, ll, cctx, apiURL, getCfg, getState, getTokenSource, getHTTPClient, getConnectOpts, notifyUnableToIngest)
				if err != nil {
					return fmt.Errorf("can't send logs: %v", err)
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
					defer cancel()
					ll.DebugContext(ctx, "flushing remote ingestion sink for up to 300ms")
					if err := remotesink.Close(ctx); err != nil {
						ll.ErrorContext(ctx, "couldn't flush buffered log", slog.Any("err", err))
					} else {
						ll.DebugContext(ctx, "done sending all logs")
					}
				}()
				loginfo("saving to %s", apiURL)
				sink = teesink.NewTeeSink(sink, remotesink)
			}

			if expcfg != nil && expcfg.ServeLocalhost != nil {
				localhostCfg := expcfg.ServeLocalhost
				state := getState(cctx)
				// TODO(antoine): all logs to a single location, right now there's code logging
				// randomly everywhere
				ll := getLogger(cctx)
				var machineID uint64
				for state.MachineID == nil {
					// TODO(antoine): if an environment token exists, auto-onboard the machine.
					if isTerminal(os.Stdout) {
						// no machine ID assigned, ensure machine gets onboarded via the login flow
						_, err := ensureLoggedIn(ctx, cctx, state, getTokenSource(cctx), apiURL, getHTTPClient(cctx, apiURL), getConnectOpts(cctx))
						if err != nil {
							return fmt.Errorf("this feature requires a valid machine ID, which requires an environment. failed to login: %v", err)
						}
					} else {
						// pick a random ID. machines are unique within environments so the odds of
						// collision are near 0
						r := rand.New(rand.NewSource(time.Now().UnixNano()))
						state.MachineID = ptr(r.Int63())
					}
				}

				machineID = uint64(*state.MachineID)
				localhostSink, done, err := dialLocalhostServer(
					ctx, ll, machineID, int(localhostCfg.Port),
					getLocalhostHTTPClient(cctx),
					func(err error) {
						logerror("unable to ingest logs with localhost: %v", err)
					},
				)
				if err != nil {
					logerror("failed to start localhost service: %v", err)
				} else {
					sink = teesink.NewTeeSink(sink, localhostSink)
					defer func() {
						ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
						defer cancel()
						ll.DebugContext(ctx, "flushing localhost ingestion sink for up to 300ms")
						if err := done(ctx); err != nil {
							ll.ErrorContext(ctx, "couldn't flush buffered log (localhost)", slog.Any("err", err))
						} else {
							ll.DebugContext(ctx, "done sending all logs")
						}
					}()
				}
			}
		}

		in := os.Stdin
		if isatty.IsTerminal(in.Fd()) {
			loginfo("reading stdin...")
		}
		go func() {
			<-ctx.Done()
			logdebug("requested to stop scanning")
			time.Sleep(500 * time.Millisecond)
			if isatty.IsTerminal(in.Fd()) {
				loginfo("Patiently waiting for stdin to send EOF (Ctrl+D). This is you! I'm reading from a TTY!")
			} else {
				// forcibly stop scanning if stuck on stdin
				logdebug("forcibly closing stdin")
				in.Close()
			}
		}()

		if err := humanlog.Scan(ctx, in, sink, handlerOpts); err != nil {
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
