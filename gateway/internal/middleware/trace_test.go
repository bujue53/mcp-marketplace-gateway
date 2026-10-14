package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContinuesW3CParentAndCorrelatesLogs(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	}()

	const traceID = "1234567890abcdef1234567890abcdef"
	const parentID = "1234567890abcdef"
	var loggedID string
	router := gin.New()
	router.Use(Trace())
	router.GET("/tools/:id", func(c *gin.Context) {
		loggedID = TraceFrom(c.Request.Context())
		if loggedID != trace.SpanFromContext(c.Request.Context()).SpanContext().TraceID().String() {
			t.Error("log correlation does not use the active span's trace ID")
		}
		c.Status(http.StatusForbidden)
	})
	req := httptest.NewRequest(http.MethodGet, "/tools/42", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-"+parentID+"-01")
	req.Header.Set("X-Trace-Id", "caller-request-id")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans; want one request span", len(spans))
	}
	span := spans[0]
	if span.SpanKind() != trace.SpanKindServer || !span.Parent().IsRemote() || span.Parent().SpanID().String() != parentID {
		t.Fatal("request did not continue the remote W3C parent")
	}
	if loggedID != traceID || response.Header().Get("X-Trace-Id") != traceID {
		t.Fatal("response and audit correlation IDs do not match the actual trace")
	}
	if span.Name() != "GET /tools/:id" || span.Status().Code != codes.Error {
		t.Fatal("request span lacks its route name or forbidden error status")
	}

	panicRouter := gin.New()
	panicRouter.Use(Recover(), Trace())
	panicRouter.GET("/panic", func(c *gin.Context) { panic("test panic") })
	panicResponse := httptest.NewRecorder()
	panicRouter.ServeHTTP(panicResponse, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if panicResponse.Code != http.StatusInternalServerError {
		t.Fatal("outer recovery did not receive the panic")
	}
	panicSpans := recorder.Ended()
	if len(panicSpans) != 2 || panicSpans[1].Status().Code != codes.Error || len(panicSpans[1].Events()) == 0 {
		t.Fatal("panic did not produce a completed request span with an error event")
	}
}
