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
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/logsvcsink"
	"github.com/urfave/cli"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ingest(
	ctx context.Context,
	ll *slog.Logger,
	cctx *cli.Context,
	apiAddr string,
	getCfg func(*cli.Context) *config.Config,
	getState func(*cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getHTTPClient func(*cli.Context) *http.Client,
) (sink.Sink, error) {
	state := getState(cctx)
	tokenSource := getTokenSource(cctx)
	httpClient := getHTTPClient(cctx)

	if state.IngestionToken == nil || time.Now().After(state.IngestionToken.ExpiresAt.AsTime()) {
		// we need to create an account token
		accountToken, err := createIngestionToken(ctx, ll, cctx, state, tokenSource, apiAddr, httpClient)
		if err != nil {
			return nil, fmt.Errorf("no ingestion token configured, and couldn't generate one: %v", err)
		}
		state.IngestionToken = accountToken
		if err := state.WriteBack(); err != nil {
			return nil, fmt.Errorf("writing back generated ingestion token: %v", err)
		}
	}

	if state.MachineID == nil || *state.MachineID <= 0 {
		//lint:ignore ST1005 "user facing call-to-action"
		return nil, fmt.Errorf("It looks like this machine isn't associated with this account. Try to login again, or register with humanlog.io.")
	}

	clOpts := []connect.ClientOption{
		connect.WithInterceptors(
			auth.NewAccountAuthInterceptor(state.IngestionToken),
		),
		connect.WithGRPC(),
	}

	client := ingestv1connect.NewIngestServiceClient(httpClient, apiAddr, clOpts...)
	var snk sink.Sink
	switch sinkType := os.Getenv("SINK_TYPE"); sinkType {
	case "unary":
		snk = logsvcsink.StartUnarySink(ctx, client, "api", uint64(*state.MachineID), 1<<20, 100*time.Millisecond, true)
	case "bidi":
		snk = logsvcsink.StartBidiStreamSink(ctx, client, "api", uint64(*state.MachineID), 1<<20, 100*time.Millisecond, true)
	case "stream":
		fallthrough // use the stream sink as default, it's the best tradeoff for performance and compatibility
	default:
		snk = logsvcsink.StartStreamSink(ctx, client, "api", uint64(*state.MachineID), 1<<20, 100*time.Millisecond, true)
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
) (*typesv1.AccountToken, error) {
	_, err := ensureLoggedIn(ctx, cctx, state, tokenSource, apiURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("ensuring you're logged in: %v", err)
	}
	if state.AccountID == nil {
		//lint:ignore ST1005 "user facing call-to-action"
		return nil, fmt.Errorf("It looks like no account is associated with this user. Try to login again, or register with humanlog.io.")
	}

	// userToken is most likely valid and unexpired, use it
	// to generate an account token with the right roles
	clOpts := connect.WithInterceptors(
		auth.Interceptors(ll, tokenSource)...,
	)
	tokenClient := tokenv1connect.NewTokenServiceClient(httpClient, apiURL, clOpts)

	expiresAt, err := hubAskTokenExpiry("Creating an ingestion token.")
	if err != nil {
		return nil, err
	}
	req := &tokenv1.GenerateAccountTokenRequest{
		AccountId: *state.AccountID,
		ExpiresAt: timestamppb.New(expiresAt),
		Roles:     []typesv1.AccountRole{typesv1.AccountRole_AccountRole_Ingestor},
	}
	res, err := tokenClient.GenerateAccountToken(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("generating account token for ingestion: %v", err)
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
			huh.NewOption("a thousand years from now =3", now.AddDate(1000, 0, 0)),
		).
		Value(&expiresAt).
		Run()
	if err != nil {
		return expiresAt, fmt.Errorf("prompting for expiry duration: %v", err)
	}
	return expiresAt, nil
}

func hubAskTokenRoles(title string) ([]typesv1.AccountRole, error) {
	var roles []typesv1.AccountRole
	err := huh.NewMultiSelect[typesv1.AccountRole]().
		Title(title).
		Description("What roles should be granted to this token?").
		Options(
			huh.NewOption("ingestor", typesv1.AccountRole_AccountRole_Ingestor),
		).
		Value(&roles).
		Run()
	if err != nil {
		return roles, fmt.Errorf("prompting for roles: %v", err)
	}
	return roles, nil
}
