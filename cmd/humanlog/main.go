package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	otelresource "go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"connectrpc.com/connect"
	"github.com/99designs/keyring"
	"github.com/aybabtme/rgbterm"
	"github.com/blang/semver"
	"github.com/charmbracelet/glamour"
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
	defaultApiURL          = "https://api.humanlog.io"
	defaultBaseSiteURL     = "https://humanlog.io"
	defaultOtlpGrpcApiAddr = "otlp-grpc.humanlog.io:443"
	defaultOtlpHttpApiURL  = "https://otlp-http.humanlog.io"
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
		Usage: "specify color mode: auto, on, off, dark, light",
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

	apiServerURL := cli.StringFlag{
		Name:   "api",
		Value:  defaultApiURL,
		Usage:  "address of the api server",
		EnvVar: "HUMANLOG_API_URL",
		Hidden: true,
	}
	baseSiteServerURL := cli.StringFlag{
		Name:   "basesite",
		Value:  defaultBaseSiteURL,
		Usage:  "address of the base site server",
		EnvVar: "HUMANLOG_BASE_SITE_URL",
		Hidden: true,
	}
	otlpGrpcApiServerAddr := cli.StringFlag{
		Name:   "otlp.grpc",
		Value:  defaultOtlpGrpcApiAddr,
		Usage:  "address of the OTLP GRPC server",
		EnvVar: "HUMANLOG_OTLP_GRPC_ADDR",
		Hidden: true,
	}
	otlpHttpApiServerURL := cli.StringFlag{
		Name:   "otlp.http",
		Value:  defaultOtlpHttpApiURL,
		Usage:  "address of the OTLP HTTP server",
		EnvVar: "HUMANLOG_OTLP_HTTP_URL",
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
	app.Email = defaultBaseSiteURL + `/support`
	app.Name = "humanlog"
	app.Version = semverVersion.String()
	app.Usage = "reads logs from stdin (and traces!), makes them pretty on stdout!"
	app.Description = renderDescription(`
# humanlog
> an observability tool on your machine.

Ingests logs and distributed tracing spans and makes them searchable and readable.

When invoked with no argument, consumes stdin and parses it, attempting to make detected logs prettier on stdout.

## Logging

You can search the logs that were parsed. Run a query with:

` + "```" + `bash
humanlog query 'summarize count() by msg'
` + "```" + `

You can also watch streams of logs being ingested with streaming
queries.

` + "```" + `bash
humanlog stream 'filter lvl == "ERROR"'
` + "```" + `

## Tracing

Similarly for distributed tracing, point your application to

` + "```" + `bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
` + "```" + `

Then query your spans and visualize them in traces.

` + "```" + `bash
humanlog query 'traces | where time > ago(30s) | take 1'
` + "```" + `

And like logs, you can watch your stream of spans:

` + "```" + `bash
humanlog stream 'traces | where service_name == "my_service"'
` + "```" + `

More complex queries are possible, same as for logs:

` + "```" + `bash
humanlog query 'traces | where time > ago(30s) | summarize span_count=count() by trace_id | sort by span_count desc | take 1'
` + "```" + `


## Ingesting data

Ingestion of logs is typically done by invoking ` + "`" + `humanlog` + "`" + ` with no argument and feeding it via stdin.

` + "```" + `bash
# Feed it via stdin
your_app | humanlog

# Or replay log files
humanlog < /var/log/yourlogfile

# pull from elsewhere for quick debugging session
kubectl logs -l app=yourapp -f | humanlog
` + "```" + `

If your application integrates twith **OpenTelemetry** you can point it to:
- OTLP/gRPC: ` + "`" + `localhost:4317` + "`" + `
- OTLP/json: ` + "`" + `localhost:4318` + "`" + `

## Getting more help

For more details:
- read [our documentation](` + defaultBaseSiteURL + `/docs/get-started/introduction).
- join [our community](` + defaultBaseSiteURL + `/link/discord).
	`)

	var (
		closers []func()

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
		otlpGrpcApiAddr  = ""
		otlpHttpApiURL   = ""
		keyringName      = "humanlog"

		resource = &types.Resource{}
		scope    = &types.Scope{}

		getCtx      = func(*cli.Context) context.Context { return ctx }
		getCfg      = func(*cli.Context) *config.Config { return cfg }
		getState    = func(*cli.Context) *state.State { return statefile }
		getResource = func(cctx *cli.Context) *types.Resource { return resource }
		getScope    = func(*cli.Context) *types.Scope { return scope }
		logOutput   = os.Stderr
		usesLogFile = false
		getLogger   = func(cctx *cli.Context) *slog.Logger {
			if cctx.Command.Name == app.Name && !usesLogFile {
				usesLogFile = true
				logdir, err := defaultLogDir(getCfg(cctx), getState(cctx))
				if err != nil {
					panic(fmt.Sprintf("looking up log dir: %v", err))
				}
				if err := os.MkdirAll(logdir, 0700); err != nil {
					panic(fmt.Sprintf("ensuring log dir exists: %v", err))
				}
				logfile := filepath.Join(logdir, "logs.json")

				logOutput, err = os.OpenFile(logfile, os.O_APPEND|os.O_CREATE|os.O_SYNC, 0640)
				if err != nil {
					panic(fmt.Sprintf("creating log file: %v", err))
				}
				closers = append(closers, func() { _ = logOutput.Close() })
			}
			return slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{
				AddSource: true,
				Level:     slogLevel(),
			}))
		}
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
				apiURL = defaultApiURL
			}
			logdebug("using api at %q", apiURL)
			return apiURL
		}
		getOTLPGRPCAPIAddr = func(*cli.Context) string {
			if otlpGrpcApiAddr == "" {
				otlpGrpcApiAddr = defaultOtlpGrpcApiAddr
			}
			logdebug("using otlp grpc at %q", otlpGrpcApiAddr)
			return otlpGrpcApiAddr
		}
		getOTLPHTTPAPIUrl = func(*cli.Context) string {
			if otlpHttpApiURL == "" {
				otlpHttpApiURL = defaultOtlpHttpApiURL
			}
			logdebug("using otlp http at %q", otlpHttpApiURL)
			return otlpHttpApiURL
		}
		getBaseSiteURL = func(*cli.Context) string {
			if baseSiteURL == "" {
				baseSiteURL = defaultBaseSiteURL
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
				logerror("invalid config file, falling back to use defaults. please fix the config file: %v", err)
				cfgFromDir, _ = config.GetDefaultConfig(defaultReleaseChannel)
			}
			cfg = cfgFromDir
		}
		if c.String(apiServerURL.Name) != "" {
			apiURL = c.String(apiServerURL.Name)
			logdebug("api URL set to %q (due to --%s flag or $%s env var)", apiURL, apiServerURL.Name, apiServerURL.EnvVar)
		}
		if c.String(otlpGrpcApiServerAddr.Name) != "" {
			otlpGrpcApiAddr = c.String(otlpGrpcApiServerAddr.Name)
			logdebug("otlp grpc addr set to %q (due to --%s flag or $%s env var)", otlpGrpcApiAddr, otlpGrpcApiServerAddr.Name, otlpGrpcApiServerAddr.EnvVar)
		}
		if c.String(otlpHttpApiServerURL.Name) != "" {
			otlpHttpApiURL = c.String(otlpHttpApiServerURL.Name)
			logdebug("otlp http URL set to %q (due to --%s flag or $%s env var)", otlpHttpApiURL, otlpHttpApiServerURL.Name, otlpHttpApiServerURL.EnvVar)
		}
		if c.String(baseSiteServerURL.Name) != "" {
			baseSiteURL = c.String(baseSiteServerURL.Name)
			logdebug("base site URL set to %q (due to --%s flag or $%s env var)", baseSiteURL, baseSiteServerURL.Name, baseSiteServerURL.EnvVar)
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

		res, err := otelresource.New(ctx,
			otelresource.WithAttributes(
				semconv.ServiceName("humanlog"),
			),
			otelresource.WithHost(),
			otelresource.WithFromEnv(),
			otelresource.WithOS(),
			otelresource.WithProcess(),
			otelresource.WithContainer(),
			otelresource.WithTelemetrySDK(),
		)
		if err != nil {
			return fmt.Errorf("detecting resource: %v", err)
		}
		resource = types.NewResource(
			"",
			types.FromOTELAttributes(res.Attributes()),
		)
		scope = types.NewScope(
			"",
			"humanlog",
			semverVersion.String(),
			nil,
		)

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
		demoCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		versionCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		authCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		apiCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getLocalhostHTTPClient, getConnectOpts),
		serviceCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		configCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		stateCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		organizationCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		environmentCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		ingestCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getOTLPGRPCAPIAddr, getOTLPHTTPAPIUrl, getHTTPClient, getConnectOpts, getResource, getScope),
		queryCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		projectCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getHTTPClient, getConnectOpts),
		streamCmd(getCtx, getLogger, getCfg, getState, getTokenSource, getAPIUrl, getBaseSiteURL, getHTTPClient, getConnectOpts),
		gennyCmd(getCtx, getLogger, getCfg, getState),
	)
	app.Flags = []cli.Flag{configFlag, skipFlag, keepFlag, sortLongest, skipUnchanged, truncates, truncateLength, colorFlag, timeFormat, ignoreInterrupts, messageFieldsFlag, timeFieldsFlag, levelFieldsFlag, apiServerURL, otlpGrpcApiServerAddr, otlpHttpApiServerURL, baseSiteServerURL, debug, useHTTP1, useProtocol}
	app.Action = func(cctx *cli.Context) error {
		if len(cctx.Args()) > 0 {
			return fmt.Errorf("unknown command: %s", strings.Join(cctx.Args(), " "))
		}
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
							logerror("can't even beeep :'( -> %v", err)
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
				remotesink, err := ingest(ingestctx, ll, cctx, apiURL, getOTLPGRPCAPIAddr, getOTLPHTTPAPIUrl, getCfg, getState, getResource, getScope, getTokenSource, getHTTPClient, getConnectOpts, notifyUnableToIngest)
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
				// TODO(antoine): all logs to a single location, right now there's code logging
				// randomly everywhere
				ll := getLogger(cctx)

				localhostSink, done, err := dialLocalhostServer(
					ctx, ll, resource, scope, int(localhostCfg.Port),
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
							ll.DebugContext(ctx, "couldn't flush buffered log (localhost)", slog.Any("err", err))
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

func renderDescription(in string) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		panic(err)
	}
	out, err := r.Render(in)
	if err != nil {
		panic(err)
	}
	return out
}
