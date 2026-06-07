// Package observability initializes OpenTelemetry tracing for the backend.
//
// Design (see docs/observability-trace-design.md):
// - OTel is infrastructure — decoupled from the notion of a business user.
// - Resource holds service-level constants only (service.name /
// service.version / deployment.environment); never user.id / tenant.id.
// - The auth middleware attaches user identity per span via
// SetAttributes, not via the resource.
// - InitTracer runs once at startup; it is unrelated to login state.
//
// An empty OTLP endpoint is supported: the tracer reduces to a no-op
// and the service still runs. Local dev gets trace_ids in logs (slog
// injects them) without shipping spans anywhere.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.31.0"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Options for InitTracer.
type Options struct {
	// ServiceName — the OTel resource service name, e.g. "attune".
	ServiceName string
	// ServiceVersion — typically the git commit hash, e.g. "5d6ea83".
	ServiceVersion string
	// Environment — deployment tier, e.g. "prod" / "dev".
	Environment string

	// Endpoint — host of the OTLP HTTP receiver (no path). Empty string ⇒ noop tracer.
	// (trace_ids still get injected into slog, just not exported).
	// e.g. "otel-collector.example.com:4318" or any self-hosted OTLP HTTP endpoint.
	Endpoint string
	// URLPath — the OTLP HTTP path, e.g. "/opentelemetry/v1/traces".
	URLPath string
	// Headers — extra OTLP request headers required by the receiver.
	Headers map[string]string
	// Insecure — true for plain HTTP, false for HTTPS.
	Insecure bool
}

// ShutdownFunc — call at process shutdown to flush buffered spans.
type ShutdownFunc func(context.Context) error

// InitTracer wires the global OTel TracerProvider + propagator.
// An empty Endpoint yields a noop tracer (suitable for local dev) that doesn't export.
func InitTracer(ctx context.Context, opts Options) (ShutdownFunc, error) {
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(opts.ServiceName),
			semconv.ServiceVersion(opts.ServiceVersion),
			semconv.DeploymentEnvironmentName(opts.Environment),
		),
		resource.WithFromEnv(), // allow env to add to OTEL_RESOURCE_ATTRIBUTES
		resource.WithProcess(), // auto-add process.pid / process.runtime
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	// W3C tracecontext propagator for cross-process propagation. Baggage is
	// included so business code can attach custom fields (e.g. tenant_id).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Empty Endpoint → noop tracer. We still create a TracerProvider so
	// SpanFromContext returns a valid SpanContext; without a BatchSpanProcessor
	// every ended span is simply discarded.
	if opts.Endpoint == "" {
		slog.InfoContext(ctx, "otel: noop tracer (no OTEL_EXPORTER_OTLP_ENDPOINT)",
			"service", opts.ServiceName)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithIDGenerator(ptrext.Of(ReadableIDGenerator{})), // timestamp-prefix trace_id
			// No SpanProcessor — ended spans are discarded.
		)
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	}

	exporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(opts.Endpoint),
		otlptracehttp.WithURLPath(opts.URLPath),
	}
	if opts.Insecure {
		exporterOpts = append(exporterOpts, otlptracehttp.WithInsecure())
	}
	if len(opts.Headers) > 0 {
		exporterOpts = append(exporterOpts, otlptracehttp.WithHeaders(opts.Headers))
	}

	exporter, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("otel exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(
			exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		// Small fleet — sample everything. Move to ParentBased(TraceIDRatioBased(...)) when costs warrant.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithIDGenerator(ptrext.Of(ReadableIDGenerator{})), // timestamp-prefix trace_id
	)
	otel.SetTracerProvider(tp)

	slog.InfoContext(
		ctx, "otel: tracer initialized",
		"service", opts.ServiceName,
		"endpoint", opts.Endpoint,
		"path", opts.URLPath,
	)

	return tp.Shutdown, nil
}
