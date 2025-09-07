package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/charmbracelet/huh"
	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	tokenv1 "github.com/humanlogio/api/go/svc/token/v1"
	"github.com/humanlogio/api/go/svc/token/v1/tokenv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/logsvcsink"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ingestCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(cctx *cli.Context) []connect.ClientOption,
	getResource func(cctx *cli.Context) *typesv1.Resource,
	getScope func(*cli.Context) *typesv1.Scope,
) cli.Command {
	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      environmentCmdName,
		ShortName: "ingest",
		Usage:     "Ingest logs into an environments.",
		Before: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)
			clOpts := getConnectOpts(cctx)
			_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
			if err != nil {
				return err
			}
			return nil
		},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			ll := getLogger(cctx)
			apiURL := getAPIUrl(cctx)
			cfg := getCfg(cctx)

			flushTimeout := 300 * time.Millisecond
			ingestctx, ingestcancel := context.WithCancel(context.WithoutCancel(ctx))
			go func() {
				<-ctx.Done()
				time.Sleep(2 * flushTimeout) // give it 2x timeout to flush before nipping the ctx entirely
				ingestcancel()
			}()
			notifyUnableToIngest := func(err error) {
				logerror("unable to ingest: %v", err)
				ingestcancel()
			}
			remotesink, err := ingest(ingestctx, ll, cctx, apiURL, getCfg, getState, getResource, getScope, getTokenSource, getHTTPClient, getConnectOpts, notifyUnableToIngest)
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

			handlerOpts := humanlog.HandlerOptionsFrom(cfg.Parser)
			if err := humanlog.Scan(ctx, in, remotesink, handlerOpts); err != nil {
				logerror("scanning caught an error: %v", err)
			}

			return nil
		},
	}
}

func ingest(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	apiURL string,
	getCfg func(*cli.Context) *config.Config,
	getState func(*cli.Context) *state.State,
	getResource func(*cli.Context) *typesv1.Resource,
	getScope func(*cli.Context) *typesv1.Scope,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getHTTPClient func(*cli.Context, string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
	notifyUnableToIngest func(error),
) (sink.Sink, error) {
	state := getState(cctx)
	tokenSource := getTokenSource(cctx)
	httpClient := getHTTPClient(cctx, apiURL)
	clOpts := getConnectOpts(cctx)

	if state.IngestionToken == nil || time.Now().After(state.IngestionToken.ExpiresAt.AsTime()) {
		// we need to create an environment token
		environmentToken, err := createIngestionToken(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, clOpts)
		if err != nil {
			return nil, fmt.Errorf("no ingestion token configured, and couldn't generate one: %v", err)
		}
		state.IngestionToken = environmentToken
		if err := state.WriteBack(); err != nil {
			return nil, fmt.Errorf("writing back generated ingestion token: %v", err)
		}
	}

	resource := getResource(cctx)
	scope := getScope(cctx)

	clOpts = append(clOpts,
		connect.WithInterceptors(auth.NewEnvironmentAuthInterceptor(ll, state.IngestionToken)),
		connect.WithGRPC(),
	)

	client := ingestv1connect.NewIngestServiceClient(httpClient, apiURL, clOpts...)
	var snk sink.Sink
	switch sinkType := os.Getenv("HUMANLOG_SINK_TYPE"); sinkType {
	case "unary":
		snk = logsvcsink.StartUnarySink(ctx, ll, client, "api", resource, scope, 1<<20, 100*time.Millisecond, true, notifyUnableToIngest)
	case "stream":
		fallthrough // use the stream sink as default, it's the best tradeoff for performance and compatibility
	default:
		snk = logsvcsink.StartStreamSink(ctx, ll, client, "api", resource, scope, 1<<20, 100*time.Millisecond, true, notifyUnableToIngest)
	}

	return snk, nil
}

func createIngestionToken(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	state *state.State,
	tokenSource *auth.UserRefreshableTokenSource,
	apiURL string,
	httpClient *http.Client,
	clOpts []connect.ClientOption,
) (*typesv1.EnvironmentToken, error) {
	_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient, clOpts)
	if err != nil {
		return nil, fmt.Errorf("ensuring you're logged in: %v", err)
	}
	envID, err := ensureEnvironmentSelected(ctx, ll, cctx, state, tokenSource, apiURL, httpClient, clOpts)
	if err != nil {
		return nil, fmt.Errorf("ensuring you've selected an environment: %v", err)
	}

	// userToken is most likely valid and unexpired, use it
	// to generate an environment token with the right roles
	clOpts = append(clOpts, connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	))
	tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts...)

	expiresAt, err := hubAskTokenExpiry("Creating an ingestion token.")
	if err != nil {
		return nil, err
	}
	req := &tokenv1.GenerateEnvironmentTokenRequest{
		EnvironmentId: envID,
		ExpiresAt:     timestamppb.New(expiresAt),
		Roles:         []typesv1.EnvironmentRole{typesv1.EnvironmentRole_EnvironmentRole_Ingestor},
	}
	res, err := tokenClient.GenerateEnvironmentToken(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("generating environment token for ingestion: %v", err)
	}
	return res.Msg.Token, nil
}

func hubAskTokenExpiry(title string) (time.Time, error) {
	var (
		now       = time.Now()
		expiresAt time.Time
	)
	err := huh.NewSelect[time.Time]().
		Title(title).
		Description("When should this token expire?").
		Options(
			huh.NewOption("in 24h", now.AddDate(0, 0, 1)),
			huh.NewOption("in a week", now.AddDate(0, 0, 7)),
			huh.NewOption("in a month", now.AddDate(0, 1, 0)),
			huh.NewOption("in 6 months", now.AddDate(0, 6, 0)),
			huh.NewOption("in a year", now.AddDate(1, 0, 0)),
		).
		Value(&expiresAt).
		Run()
	if err != nil {
		return expiresAt, fmt.Errorf("prompting for expiry duration: %v", err)
	}
	return expiresAt, nil
}

func hubAskTokenRoles(title string) ([]typesv1.EnvironmentRole, error) {
	var roles []typesv1.EnvironmentRole
	err := huh.NewMultiSelect[typesv1.EnvironmentRole]().
		Title(title).
		Description("What roles should be granted to this token?").
		Options(
			huh.NewOption("ingestor", typesv1.EnvironmentRole_EnvironmentRole_Ingestor),
		).
		Value(&roles).
		Run()
	if err != nil {
		return roles, fmt.Errorf("prompting for roles: %v", err)
	}
	return roles, nil
}
