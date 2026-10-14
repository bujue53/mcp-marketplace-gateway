package otel

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init 初始化全局 TracerProvider，把 span 导出到 OTLP（Jaeger / Collector）。
// endpoint 为空时保留本地追踪上下文，但不向外部导出。
func Init(ctx context.Context, serviceName, endpoint string) (func(), error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return func() {}, err
	}
	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if endpoint != "" {
		endpointURL := endpoint
		if !strings.Contains(endpointURL, "://") {
			endpointURL = "http://" + endpointURL
		}
		endpointURL = strings.TrimRight(endpointURL, "/")
		if !strings.HasSuffix(endpointURL, "/v1/traces") {
			endpointURL += "/v1/traces"
		}
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpointURL))
		if err != nil {
			return func() {}, err
		}
		options = append(options, sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)))
	}
	tp := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
	}, nil
}
