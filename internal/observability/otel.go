// Package observability initializes OpenTelemetry tracing for BE.
//
// 设计原则(详见 docs/observability-trace-design.md):
//   - OTel 是基础设施,跟"业务用户"身份完全解耦
//   - Resource 只描述 service-level 常量(service.name / service.version /
//     deployment.environment),不放 user.id / tenant.id
//   - user.id 由 auth.JWTMiddleware 在每个 span 上动态 SetAttributes,不是 resource
//   - InitTracer 一次性 init,跟 user login/logout 无关
//
// PR-1a 阶段:OTLP endpoint 留空也能正常启动(noop tracer)。
//   - prod 部署后在 .env 配 OTEL_EXPORTER_OTLP_ENDPOINT 才真上报到 SLS Trace
//   - 本地开发不配 endpoint,仍产 trace_id(slog 注入),只是不上报
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
)

// Options for InitTracer.
type Options struct {
	// ServiceName 服务名,如 "casceneai-be" / "casceneai-gateway"。
	ServiceName string
	// ServiceVersion 服务版本(通常是 git commit hash),如 "5d6ea83"。
	ServiceVersion string
	// Environment 部署环境,如 "prod" / "dev"。
	Environment string

	// Endpoint OTLP HTTP 端点的 host(不带 path)。空字符串 = noop tracer
	// (不上报,但仍产 trace_id 注入到 slog)。
	// 例: "casceneai-prod.cn-shenzhen-intranet.log.aliyuncs.com"
	Endpoint string
	// URLPath OTLP HTTP 路径。SLS Trace 服务用 "/opentelemetry/v1/traces"。
	URLPath string
	// Headers OTLP 请求头(SLS 走自定义 header,如 x-sls-otel-project)。
	Headers map[string]string
	// Insecure 用 HTTP(true)还是 HTTPS(false)。SLS 内网入口用 HTTP。
	Insecure bool
}

// ShutdownFunc 进程退出前调用,刷干 buffer。
type ShutdownFunc func(context.Context) error

// InitTracer 初始化全局 OTel TracerProvider + propagator。
// Endpoint 空时返回 noop tracer(本地开发可用),不向 SLS 上报。
func InitTracer(ctx context.Context, opts Options) (ShutdownFunc, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(opts.ServiceName),
			semconv.ServiceVersion(opts.ServiceVersion),
			semconv.DeploymentEnvironmentName(opts.Environment),
		),
		resource.WithFromEnv(), // 允许 env 补充 OTEL_RESOURCE_ATTRIBUTES
		resource.WithProcess(), // 自动加 process.pid / process.runtime
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	// W3C tracecontext propagator(跨进程透传)。Baggage 留口子,业务想透传
	// 自定义字段(如 tenant_id)可以加。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Endpoint 空 → noop tracer。仍创建 TracerProvider 让 SpanFromContext 能
	// 拿到有效 SpanContext,但 BatchSpanProcessor 用 noop exporter 直接丢弃。
	if opts.Endpoint == "" {
		slog.InfoContext(ctx, "otel: noop tracer (no OTEL_EXPORTER_OTLP_ENDPOINT)",
			"service", opts.ServiceName)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithIDGenerator(&ReadableIDGenerator{}), // 时间戳前缀 trace_id
			// 不加 SpanProcessor → span End 后直接丢
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
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		// 体量小,先全采。月费失控时改 ParentBased(TraceIDRatioBased(0.1))。
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithIDGenerator(&ReadableIDGenerator{}), // 时间戳前缀 trace_id
	)
	otel.SetTracerProvider(tp)

	slog.InfoContext(ctx, "otel: tracer initialized",
		"service", opts.ServiceName,
		"endpoint", opts.Endpoint,
		"path", opts.URLPath,
	)

	return tp.Shutdown, nil
}
