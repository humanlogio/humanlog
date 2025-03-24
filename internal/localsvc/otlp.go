package localsvc

import (
	"context"

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
	return otlp.svc.storage.ExportLogs(ctx, req)
}

type MetricsOTLP struct {
	svc *Service
	otlpmetricssvcpb.UnimplementedMetricsServiceServer
}

func newMetricsOTLP(svc *Service) *MetricsOTLP {
	return &MetricsOTLP{svc: svc}
}

func (otlp *MetricsOTLP) Export(ctx context.Context, req *otlpmetricssvcpb.ExportMetricsServiceRequest) (*otlpmetricssvcpb.ExportMetricsServiceResponse, error) {
	return otlp.svc.storage.ExportMetrics(ctx, req)
}

type TracingOTLP struct {
	svc *Service
	otlptracesvcpb.UnimplementedTraceServiceServer
}

func newTracingOTLP(svc *Service) *TracingOTLP {
	return &TracingOTLP{svc: svc}
}

func (otlp *TracingOTLP) Export(ctx context.Context, req *otlptracesvcpb.ExportTraceServiceRequest) (*otlptracesvcpb.ExportTraceServiceResponse, error) {
	return otlp.svc.storage.ExportTraces(ctx, req)
}
