package otel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func recordingProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return provider, recorder
}

func TestTransportPropagatesTraceAndIncludesResponseBody(t *testing.T) {
	provider, recorder := recordingProvider(t)
	var received trace.SpanContext
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header)))
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("temporary failure"))
	}))
	defer server.Close()
	ctx, parent := provider.Tracer("test").Start(context.Background(), "caller")
	defer parent.End()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/tool?private=value", nil)
	resp, err := (&http.Client{Transport: NewTransport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.Ended()) != 0 {
		t.Fatal("client span ended before response body was consumed")
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d ended spans; want one HTTP client span", len(spans))
	}
	span := spans[0]
	if span.SpanKind() != trace.SpanKindClient || span.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("HTTP span is not a child of the caller")
	}
	if !received.IsRemote() || received.TraceID() != parent.SpanContext().TraceID() || received.SpanID() != span.SpanContext().SpanID() {
		t.Fatal("downstream traceparent does not identify the HTTP client span")
	}
	if req.Header.Get("traceparent") != "" {
		t.Fatal("transport modified the original request headers")
	}
	if span.Status().Code != codes.Error {
		t.Fatal("HTTP 503 was not recorded as a span error")
	}
}

type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestTransportFailureEndsErrorSpan(t *testing.T) {
	_, recorder := recordingProvider(t)
	req, _ := http.NewRequest(http.MethodGet, "http://example.test/tool", nil)
	_, err := NewTransport(failingRoundTripper{}).RoundTrip(req)
	if err == nil {
		t.Fatal("expected transport error")
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error || len(spans[0].Events()) == 0 {
		t.Fatal("connection failure did not produce a completed span with an error event")
	}
}

func TestInitWithoutExporterKeepsTraceCorrelation(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	defer otel.SetTracerProvider(previousProvider)
	defer otel.SetTextMapPropagator(previousPropagator)
	shutdown, err := Init(context.Background(), "test-service", "")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	_, span := otel.Tracer("test").Start(context.Background(), "local")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("missing exporter disabled trace ID generation")
	}
}
