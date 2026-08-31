package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init 初始化全局 TracerProvider，把 span 导出到 OTLP（Jaeger / Collector）。
// 当 endpoint 为空时返回 no-op provider，避免依赖未部署时崩溃。
func Init(ctx context.Context, serviceName, endpoint string) (func(), error) {
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL("http://"+endpoint),
		otlptracehttp.WithProtocol(otlptracehttp.WithHTTPProtobuf()),
	)
	if err != nil {
		// OTLP 不可用时静默降级为 no-op。
		return func() {}, nil
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithExportInterval(time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return func() { _ = tp.Shutdown(context.Background()) }, nil
}

