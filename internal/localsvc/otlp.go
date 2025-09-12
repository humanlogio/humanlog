package localsvc

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

var (
	// otlp compatibility
	_ otlplogssvcpb.LogsServiceServer       = (*LoggingOTLP)(nil)
	_ otlptracesvcpb.TraceServiceServer     = (*TracingOTLP)(nil)
	_ otlpmetricssvcpb.MetricsServiceServer = (*MetricsOTLP)(nil)
)

type LoggingOTLP struct {
	svc *Service
	otlplogssvcpb.UnimplementedLogsServiceServer
}

func newLoggingOTLP(svc *Service) *LoggingOTLP {
	return &LoggingOTLP{svc: svc}
}

func (otlp *LoggingOTLP) Export(ctx context.Context, req *otlplogssvcpb.ExportLogsServiceRequest) (*otlplogssvcpb.ExportLogsServiceResponse, error) {
	ll := otlp.svc.ll.WithGroup("OTLP.Logging.Export")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()
	return otlp.svc.storage.ExportLogs(ctx, req)
}

func (otlp *LoggingOTLP) ExportHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ll := otlp.svc.ll.WithGroup("OTLP.Logging.ExportHTTP")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()

	enc, ok := readContentType(w, r)
	if !ok {
		return
	}

	body, ok := readAndCloseBody(w, r, enc)
	if !ok {
		return
	}

	otlpReq, err := enc.unmarshalLogsRequest(body)
	if err != nil {
		writeError(w, enc, err, http.StatusBadRequest)
		return
	}

	otlpResp, err := otlp.svc.storage.ExportLogs(ctx, otlpReq)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}

	msg, err := enc.marshalLogsResponse(otlpResp)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}
	writeResponse(w, enc.contentType(), http.StatusOK, msg)

}

type MetricsOTLP struct {
	svc *Service
	otlpmetricssvcpb.UnimplementedMetricsServiceServer
}

func newMetricsOTLP(svc *Service) *MetricsOTLP {
	return &MetricsOTLP{svc: svc}
}

func (otlp *MetricsOTLP) Export(ctx context.Context, req *otlpmetricssvcpb.ExportMetricsServiceRequest) (*otlpmetricssvcpb.ExportMetricsServiceResponse, error) {
	ll := otlp.svc.ll.WithGroup("OTLP.Metrics.Export")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()
	return otlp.svc.storage.ExportMetrics(ctx, req)
}

func (otlp *MetricsOTLP) ExportHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ll := otlp.svc.ll.WithGroup("OTLP.Metrics.ExportHTTP")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()

	enc, ok := readContentType(w, r)
	if !ok {
		return
	}

	body, ok := readAndCloseBody(w, r, enc)
	if !ok {
		return
	}

	otlpReq, err := enc.unmarshalMetricsRequest(body)
	if err != nil {
		writeError(w, enc, err, http.StatusBadRequest)
		return
	}

	otlpResp, err := otlp.svc.storage.ExportMetrics(ctx, otlpReq)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}

	msg, err := enc.marshalMetricsResponse(otlpResp)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}
	writeResponse(w, enc.contentType(), http.StatusOK, msg)
}

type TracingOTLP struct {
	svc *Service
	otlptracesvcpb.UnimplementedTraceServiceServer
}

func newTracingOTLP(svc *Service) *TracingOTLP {
	return &TracingOTLP{svc: svc}
}

func (otlp *TracingOTLP) Export(ctx context.Context, req *otlptracesvcpb.ExportTraceServiceRequest) (*otlptracesvcpb.ExportTraceServiceResponse, error) {
	ll := otlp.svc.ll.WithGroup("OTLP.Tracing.Export")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()
	return otlp.svc.storage.ExportTraces(ctx, req)
}

func (otlp *TracingOTLP) ExportHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ll := otlp.svc.ll.WithGroup("OTLP.Tracing.ExportHTTP")
	ll.InfoContext(ctx, "starting")
	start := time.Now()
	defer func() { ll.InfoContext(ctx, "done", slog.Duration("duration", time.Since(start))) }()

	enc, ok := readContentType(w, r)
	if !ok {
		return
	}

	body, ok := readAndCloseBody(w, r, enc)
	if !ok {
		return
	}

	otlpReq, err := enc.unmarshalTracesRequest(body)
	if err != nil {
		writeError(w, enc, err, http.StatusBadRequest)
		return
	}

	otlpResp, err := otlp.svc.storage.ExportTraces(ctx, otlpReq)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}

	msg, err := enc.marshalTracesResponse(otlpResp)
	if err != nil {
		writeError(w, enc, err, http.StatusInternalServerError)
		return
	}
	writeResponse(w, enc.contentType(), http.StatusOK, msg)
}
