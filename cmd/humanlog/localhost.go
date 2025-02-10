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
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/sink/logsvcsink"
)

func dialLocalhostServer(
	ctx context.Context,
	ll *slog.Logger,
	machineID uint64,
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
	localhostSink := logsvcsink.StartStreamSink(ctx, ll, client, "local", machineID, 1<<20, 100*time.Millisecond, true, notifyUnableToIngest)
	return localhostSink, func(ctx context.Context) error {
		logdebug("flushing localhost sink")
		return localhostSink.Close(ctx)
	}, nil
}
