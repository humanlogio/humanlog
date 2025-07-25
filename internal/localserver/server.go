package localserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"time"

	"connectrpc.com/connect"

	connectcors "connectrpc.com/cors"
	"connectrpc.com/otelconnect"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	"github.com/humanlogio/api/go/svc/alert/v1/alertv1connect"
	"github.com/humanlogio/api/go/svc/dashboard/v1/dashboardv1connect"
	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	"github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	stackv1 "github.com/humanlogio/api/go/svc/stack/v1"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/errutil"
	"github.com/humanlogio/humanlog/internal/localalert"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/localsvc"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/retry"
	"github.com/rs/cors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/contrib/propagators/ot"
	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

func ServeLocalhost(
	ctx context.Context,
	ll *slog.Logger,
	localhostCfg *typesv1.ServeLocalhostConfig,
	ownVersion *typesv1.Version,
	app *localstorage.AppCtx,
	openStorage func(ctx context.Context) (localstorage.Storage, error),
	openState func(ctx context.Context, db localstorage.Storage) (localstate.DB, error),
	registerOnCloseServer func(srv *http.Server),
	doLogin func(ctx context.Context, returnToURL string) error,
	doLogout func(ctx context.Context, returnToURL string) error,
	doUpdate func(ctx context.Context) error,
	doRestart func(ctx context.Context) error,
	getConfig func(ctx context.Context) (*typesv1.LocalhostConfig, error),
	setConfig func(ctx context.Context, cfg *typesv1.LocalhostConfig) error,
	whoami func(ctx context.Context) (*userv1.WhoamiResponse, error),
	notifyAlert func(ctx context.Context, ar *typesv1.AlertRule, as *typesv1.AlertState, o *typesv1.Obj) error,
) error {
	port := int(localhostCfg.Port)

	// obtaining the listener is our way of also getting an exclusive lock on the storage engine
	// although if someone was independently using the DB before we started, we'll be holding the listener
	// lock while failing to open the storage... this will cause the service to exit
	localhostAddr := net.JoinHostPort("localhost", strconv.Itoa(port))
	var (
		l           net.Listener
		err         error
		attemptLeft = 10
	)
	err = retry.Do(ctx, func(ctx context.Context) (bool, error) {
		ll.InfoContext(ctx, "requesting listener for address", slog.String("addr", localhostAddr))
		l, err = net.Listen("tcp", localhostAddr)
		if err != nil && !errutil.IsEADDRINUSE(err) {
			return false, fmt.Errorf("listening on host/port: %v", err)
		}
		if errutil.IsEADDRINUSE(err) {
			// try again
			ll.InfoContext(ctx, "address in use, retrying later", slog.Int("attempts", attemptLeft))
			attemptLeft--
			return attemptLeft > 0, nil
		}
		return false, nil
	}, retry.UseBaseSleep(20*time.Millisecond), retry.UseCapSleep(time.Second))
	if err != nil {
		return fmt.Errorf("unable to obtain localhost listener: %v", err)
	}
	if l == nil {
		return fmt.Errorf("never obtained listener, giving up")
	}
	defer l.Close()
	ll.InfoContext(ctx, "obtained listener")

	var otlpGrpcL net.Listener
	if localhostCfg.Otlp != nil {
		otlpGrpcPort := int(localhostCfg.Otlp.GrpcPort)
		localhostOtlpAddr := net.JoinHostPort("localhost", strconv.Itoa(otlpGrpcPort))
		ll.InfoContext(ctx, "requesting listener for address (OTLP gRPC service)", slog.String("addr", localhostOtlpAddr))
		otlpGrpcL, err = net.Listen("tcp", localhostOtlpAddr)
		if err != nil {
			return fmt.Errorf("listening on OTLP gRPC port: %v", err)
		}
		defer otlpGrpcL.Close()
		ll.InfoContext(ctx, "obtained OTLP gRPC listener")
	}

	var otlpHttpL net.Listener
	if localhostCfg.Otlp != nil && localhostCfg.Otlp.HttpPort != localhostCfg.Port {
		otlpHttpPort := int(localhostCfg.Otlp.HttpPort)
		localhostOtlpAddr := net.JoinHostPort("localhost", strconv.Itoa(otlpHttpPort))
		ll.InfoContext(ctx, "requesting listener for address (OTLP HTTP service)", slog.String("addr", localhostOtlpAddr))
		otlpHttpL, err = net.Listen("tcp", localhostOtlpAddr)
		if err != nil {
			return fmt.Errorf("listening on OTLP HTTP port: %v", err)
		}
		defer otlpHttpL.Close()
		ll.InfoContext(ctx, "obtained OTLP HTTP listener")
	}

	ll.InfoContext(ctx, "opening storage engine")
	storage, err := openStorage(ctx)
	if err != nil {
		return fmt.Errorf("opening localstorage %q: %v", localhostCfg.Engine, err)
	}
	defer func() {
		ll.InfoContext(ctx, "closing storage engine")
		if err := storage.Close(); err != nil {
			ll.ErrorContext(ctx, "unable to cleanly close storage engine", slog.Any("err", err))
		} else {
			ll.InfoContext(ctx, "storage engine closed cleanly")
		}
	}()
	ll.InfoContext(ctx, "opening state engine")
	state, err := openState(ctx, storage)
	if err != nil {
		return fmt.Errorf("opening localstorage %q: %v", localhostCfg.Engine, err)
	}

	ll.InfoContext(ctx, "preparing localhost services")

	mux := http.NewServeMux()

	localhostsvc := localsvc.New(ll, ownVersion, storage, state,
		doLogin,
		doLogout,
		doUpdate,
		doRestart,
		getConfig,
		setConfig,
		whoami,
	)

	otelIctpr, err := otelconnect.NewInterceptor()
	if err != nil {
		return fmt.Errorf("setting up otel interceptors: %v", err)
	}

	mux.Handle(localhostv1connect.NewLocalhostServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))
	mux.Handle(ingestv1connect.NewIngestServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))
	mux.Handle(queryv1connect.NewQueryServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))
	mux.Handle(queryv1connect.NewTraceServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))
	mux.Handle(dashboardv1connect.NewDashboardServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))
	mux.Handle(alertv1connect.NewAlertServiceHandler(localhostsvc, connect.WithInterceptors(otelIctpr)))

	httphdl := h2c.NewHandler(mux, &http2.Server{})
	httphdl = otelhttp.NewHandler(httphdl, "humanlog.ConnectRPC")
	httphdl = withCORS(httphdl)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "oh noes the sky is falling\n\n%s", string(debug.Stack()))
				panic(r)
			}
		}()
		httphdl.ServeHTTP(w, r)
	})}

	registerOnCloseServer(srv)

	ll.InfoContext(ctx, "serving localhost services")

	eg, ctx := errgroup.WithContext(ctx)
	if otlpGrpcL != nil {

		stats := otelgrpc.NewServerHandler(
			otelgrpc.WithMessageEvents(otelgrpc.ReceivedEvents, otelgrpc.SentEvents),
			otelgrpc.WithFilter(
				filters.Any(
					filters.Not(
						filters.ServiceName(otlptracesvcpb.TraceService_ServiceDesc.ServiceName),
					),
					filters.Not(
						filters.ServiceName(otlpmetricssvcpb.MetricsService_ServiceDesc.ServiceName),
					),
				),
			),
		)

		// otel gRPC receiver handlers
		gsrv := grpc.NewServer(grpc.StatsHandler(stats))
		otlplogssvcpb.RegisterLogsServiceServer(gsrv, localhostsvc.AsLoggingOTLP())
		otlpmetricssvcpb.RegisterMetricsServiceServer(gsrv, localhostsvc.AsMetricsOTLP())
		otlptracesvcpb.RegisterTraceServiceServer(gsrv, localhostsvc.AsTracingOTLP())
		eg.Go(func() error {
			ll.InfoContext(ctx, "OTLP gRPC server starting")
			if err := gsrv.Serve(otlpGrpcL); err != nil {
				ll.ErrorContext(ctx, "otlp gRPC server errored", slog.Any("err", err))
				return err
			}
			return nil
		})
	}
	if otlpHttpL != nil {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/traces", localhostsvc.AsTracingOTLP().ExportHTTP)
		mux.HandleFunc("/v1/metrics", localhostsvc.AsMetricsOTLP().ExportHTTP)
		mux.HandleFunc("/v1/logs", localhostsvc.AsLoggingOTLP().ExportHTTP)
		// mux.HandleFunc("/v1development/profiles", localhostsvc.AsProfileOTLP().ExportHTTP)

		srv := http.Server{Handler: withCORS(mux)}
		eg.Go(func() error {
			ll.InfoContext(ctx, "OTLP HTTP server starting")
			if err := srv.Serve(otlpHttpL); err != nil {
				ll.ErrorContext(ctx, "otlp HTTP server errored", slog.Any("err", err))
				return err
			}
			return nil
		})
	}
	eg.Go(func() error {
		ll.InfoContext(ctx, "humanlog localhost server starting")
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ll.ErrorContext(ctx, "query engine server errored", slog.Any("err", err))
			return err
		}
		return nil
	})

	eg.Go(func() error {
		// handle alerts

		iteratorForStack := func(ctx context.Context) *iterapi.Iter[*typesv1.Stack] {
			return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.Stack, *typesv1.Cursor, error) {
				out, err := state.ListStack(ctx, &stackv1.ListStackRequest{Cursor: cursor, Limit: limit})
				if err != nil {
					return nil, nil, err
				}
				var items []*typesv1.Stack
				for _, el := range out.Items {
					items = append(items, el.Stack)
				}
				return items, out.Next, nil
			})
		}
		iteratorForAlertGroup := func(ctx context.Context, stackName string) *iterapi.Iter[*typesv1.AlertGroup] {
			return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.AlertGroup, *typesv1.Cursor, error) {
				out, err := state.ListAlertGroup(ctx, &alertv1.ListAlertGroupRequest{StackName: stackName, Cursor: cursor, Limit: limit})
				if err != nil {
					return nil, nil, err
				}
				var items []*typesv1.AlertGroup
				for _, el := range out.Items {
					items = append(items, el.AlertGroup)
				}
				return items, out.Next, nil
			})
		}

		handleAlerts := func(ctx context.Context) error {
			stackIter := iteratorForStack(ctx)
			for stackIter.Next() {
				stack := stackIter.Current()
				evaluator := localalert.NewEvaluator(storage, time.Now)

				alertGroupIter := iteratorForAlertGroup(ctx, stack.Name)
				for alertGroupIter.Next() {
					alertGroup := alertGroupIter.Current()
					if err := evaluator.EvaluateRules(ctx, stack, alertGroup, notifyAlert); err != nil {
						return fmt.Errorf("evaluating alert group %q: %v", alertGroup.Name, err)
					}
				}
				if err := alertGroupIter.Err(); err != nil {
					return fmt.Errorf("iterating alert groups: %v", err)
				}
			}
			if err := stackIter.Err(); err != nil {
				return fmt.Errorf("iterating stacks: %v", err)
			}

			return nil

		}

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		ll.InfoContext(ctx, "humanlog localhost alert monitor starting")
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := handleAlerts(ctx); err != nil {
					ll.ErrorContext(ctx, "failed to evaluate alerting rules", slog.Any("err", err))
				}
			}
		}
	})

	if err := eg.Wait(); err != nil {
		return err
	}
	ll.InfoContext(ctx, "stopped serving localhost services")

	return nil
}

func withCORS(hdl http.Handler) http.Handler {
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
		AllowedHeaders: slices.Concat(
			connectcors.AllowedHeaders(),
			[]string{"Browser-Authorization", "Request-Id"},
			ot.OT{}.Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3SingleHeader)).Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)).Fields(),
		),
		ExposedHeaders: slices.Concat(
			connectcors.ExposedHeaders(),
			[]string{"Browser-Authorization", "Request-Id"},
			ot.OT{}.Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3SingleHeader)).Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)).Fields(),
		),
		MaxAge: 7200, // 2 hours in seconds
	})
	return c.Handler(hdl)
}
