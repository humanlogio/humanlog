package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	connectcors "connectrpc.com/cors"

	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	"github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/errutil"
	"github.com/humanlogio/humanlog/internal/localsvc"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/logsvcsink"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	// imported for side-effect of `init()` registration
	_ "github.com/humanlogio/humanlog/internal/diskstorage"
	_ "github.com/humanlogio/humanlog/internal/memstorage"
)

func startLocalhostServer(
	ctx context.Context,
	ll *slog.Logger,
	cfg *config.Config,
	state *state.State,
	machineID uint64,
	port int,
	localhostHttpClient *http.Client,
	ownVersion *typesv1.Version,
	app *localstorage.AppCtx,
) (localsink sink.Sink, done func(context.Context) error, err error) {

	notifyUnableToIngest := func(err error) {
		ll.ErrorContext(ctx, "localhost ingestor is unable to ingest", slog.Any("err", err))
		// TODO: take this as a hint to become the localhost ingestor
	}

	localhostAddr := net.JoinHostPort("localhost", strconv.Itoa(port))
	l, err := net.Listen("tcp", localhostAddr)
	if err != nil && !errutil.IsEADDRINUSE(err) {
		return nil, nil, fmt.Errorf("listening on host/port: %v", err)
	}
	if errutil.IsEADDRINUSE(err) {
		// TODO(antoine):
		// 1) log to localhost until it's gone
		// 2) try to gain the socket, if fail; goto 1)
		// 3) serve the localhost service + save local logs + forward them to remote
		addr, err := url.Parse("http://" + localhostAddr)
		if err != nil {
			panic(err)
		}
		logdebug("sending logs to localhost forwarder")
		client := ingestv1connect.NewIngestServiceClient(localhostHttpClient, addr.String())
		localhostSink := logsvcsink.StartStreamSink(ctx, ll, client, "local", machineID, 1<<20, 100*time.Millisecond, true, notifyUnableToIngest)
		return localhostSink, func(ctx context.Context) error {
			logdebug("flushing localhost sink")
			return localhostSink.Close(ctx)
		}, nil
	}

	storage, err := localstorage.Open(
		ctx,
		cfg.ExperimentalFeatures.ServeLocalhost.Engine,
		ll.WithGroup("storage"),
		cfg.ExperimentalFeatures.ServeLocalhost.Cfg,
		app,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("opening localstorage %q: %v", cfg.ExperimentalFeatures.ServeLocalhost.Engine, err)
	}
	ownSink, _, err := storage.SinkFor(ctx, int64(machineID), time.Now().UnixNano())
	if err != nil {
		return nil, nil, fmt.Errorf("can't create own sink: %v", err)
	}

	mux := http.NewServeMux()

	localhostsvc := localsvc.New(ll, state, ownVersion, storage)
	mux.Handle(localhostv1connect.NewLocalhostServiceHandler(localhostsvc))
	mux.Handle(ingestv1connect.NewIngestServiceHandler(localhostsvc))
	mux.Handle(queryv1connect.NewQueryServiceHandler(localhostsvc))

	hdl := h2c.NewHandler(mux, &http2.Server{})
	hdl = withCORS(hdl)

	srv := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "oh noes the sky is falling\n\n%s", string(debug.Stack()))
				panic(r)
			}
		}()
		hdl.ServeHTTP(w, r)
	})}

	go func() {
		loginfo("localhost service available on %s, visit `https://humanlog.io` so see your logs", l.Addr().String())
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logerror("failed to serve localhost service, giving up: %v", err)
		}
	}()

	return ownSink, func(ctx context.Context) error {
		errc := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- srv.Shutdown(ctx)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- ownSink.Close(ctx)
		}()
		wg.Wait()
		close(errc)
		_ = l.Close()
		var ferr error
		for err := range errc {
			if ferr == nil {
				ferr = err
			} else {
				ll.ErrorContext(ctx, "multiple errors", slog.Any("err", err))
			}
		}
		return ferr
	}, nil
}

// withCORS adds CORS support to a Connect HTTP handler.
func withCORS(connectHandler http.Handler) http.Handler {
	c := cors.New(cors.Options{
		// Debug: true,
		AllowedOrigins: []string{
			"https://humanlog.io",
			"https://humanlog.dev",
			"https://app.humanlog.dev",
			"https://app.humanlog.dev:3000",
			"https://humanlog.sh",
			"http://localhost:3000",
			"https://humanlog.test:3000",
		},
		AllowedMethods: connectcors.AllowedMethods(),
		AllowedHeaders: connectcors.AllowedHeaders(),
		ExposedHeaders: connectcors.ExposedHeaders(),
		MaxAge:         7200, // 2 hours in seconds
	})
	return c.Handler(connectHandler)
}
