package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_requests_total",
		Help: "Total HTTP requests handled by the gateway.",
	}, []string{"method", "route", "code"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_gateway_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	toolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tool_calls_total",
		Help: "Total MCP tool calls.",
	}, []string{"tool", "status"})

	circuitOpen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_gateway_circuit_open",
		Help: "1 if the circuit breaker is open for a key.",
	}, []string{"key"})
)

func ObserveRequest(method, route string, code int, start time.Time) {
	requestsTotal.WithLabelValues(method, route, strconv.Itoa(code)).Inc()
	requestDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
}

func ObserveToolCall(tool string, code int) {
	toolCallsTotal.WithLabelValues(tool, strconv.Itoa(code)).Inc()
}

func SetCircuit(key string, open bool) {
	v := 0.0
	if open {
		v = 1
	}
	circuitOpen.WithLabelValues(key).Set(v)
}

