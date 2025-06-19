package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/logsvcsink"
)

func dialLocalhostServer(
	ctx context.Context,
	ll *slog.Logger,
	resource *typesv1.Resource,
	scope *typesv1.Scope,
	port int,
	localhostHttpClient *http.Client,
	notifyUnableToIngest func(err error),
) (localsink sink.Sink, done func(context.Context) error, err error) {
	localhostAddr := net.JoinHostPort("localhost", strconv.Itoa(port))
	addr, err := url.Parse("http://" + localhostAddr)
	if err != nil {
		panic(err)
	}
	logdebug("sending logs to localhost forwarder")
	client := ingestv1connect.NewIngestServiceClient(localhostHttpClient, addr.String())
	localhostSink := logsvcsink.StartStreamSink(ctx, ll, client, "local", resource, scope, 1<<20, 100*time.Millisecond, true, notifyUnableToIngest)
	return localhostSink, func(ctx context.Context) error {
		logdebug("flushing localhost sink")
		return localhostSink.Close(ctx)
	}, nil
}
