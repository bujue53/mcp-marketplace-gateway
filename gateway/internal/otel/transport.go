package otel

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// NewTransport 为 MCP 和 REST 下游请求建立 span，并透传 W3C traceparent。
func NewTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = pooledTransport
	}
	return &transport{base: base}
}

var pooledTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 512
	transport.MaxIdleConnsPerHost = 128
	transport.MaxConnsPerHost = 512
	return transport
}()

type transport struct{ base http.RoundTripper }

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	ctx, span := otel.Tracer("mcp-gateway/http-client").Start(req.Context(), "HTTP "+req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("http.request.method", req.Method),
			attribute.String("server.address", req.URL.Hostname()), attribute.String("url.path", req.URL.Path)),
	)
	// 克隆请求，避免修改调用方持有的 Header。
	outgoing := req.Clone(ctx)
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outgoing.Header))
	resp, err := t.base.RoundTrip(outgoing)
	if err != nil {
		metrics.RecordDownstream(req.Context(), time.Since(start))
		span.RecordError(err)
		span.SetStatus(codes.Error, "HTTP request failed")
		span.End()
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}
	// span 覆盖响应体读取时间，并保证读完或关闭时只结束一次。
	resp.Body = &tracedBody{ReadCloser: resp.Body, span: span, ctx: req.Context(), start: start}
	return resp, nil
}

type tracedBody struct {
	io.ReadCloser
	span  trace.Span
	once  sync.Once
	ctx   context.Context
	start time.Time
}

func (b *tracedBody) end() {
	b.once.Do(func() { metrics.RecordDownstream(b.ctx, time.Since(b.start)); b.span.End() })
}

func (b *tracedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		if err != io.EOF {
			b.span.RecordError(err)
			b.span.SetStatus(codes.Error, "HTTP response read failed")
		}
		b.end()
	}
	return n, err
}

func (b *tracedBody) Close() error {
	err := b.ReadCloser.Close()
	if err != nil {
		b.span.RecordError(err)
		b.span.SetStatus(codes.Error, "HTTP response close failed")
	}
	b.end()
	return err
}
