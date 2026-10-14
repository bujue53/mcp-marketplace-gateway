package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/auth"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/config"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/metrics"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// MW 聚合中间件所需的依赖。
type MW struct {
	cfg   *config.Config
	st    *store.Store
	rl    *RateLimiter
	cb    *CircuitBreaker
	audit *store.AuditSink
}

func New(cfg *config.Config, st *store.Store, rl *RateLimiter, cb *CircuitBreaker) *MW {
	return &MW{cfg: cfg, st: st, rl: rl, cb: cb, audit: store.NewAuditSink(st)}
}

func (m *MW) CloseAudit() {
	if m.audit != nil {
		m.audit.Close()
	}
}

// AllowToolCall checks whether calls for a tool are currently allowed.
func (m *MW) AllowToolCall(toolID int64) bool {
	return m.cb == nil || m.cb.Allow("tool:"+strconv.FormatInt(toolID, 10))
}

// RecordToolCall records the outcome of a tool call for circuit evaluation.
func (m *MW) RecordToolCall(toolID int64, success bool) {
	if m.cb != nil {
		m.cb.Record("tool:"+strconv.FormatInt(toolID, 10), success)
	}
}

// CORS 允许前端跨域访问。
func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Trace-Id", "traceparent", "tracestate", "baggage"},
		ExposeHeaders:    []string{"X-Trace-Id"},
		AllowCredentials: false,
	})
}

// Trace 提取上游追踪上下文，为请求建立 span，并使用真实 trace ID 关联日志。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := otel.Tracer("mcp-gateway/http-server").Start(ctx, c.Request.Method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("http.request.method", c.Request.Method)),
		)
		defer func() {
			recovered := recover()
			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			status := c.Writer.Status()
			if recovered != nil {
				// 外层 Recover 负责响应；此处为异常退出保留追踪错误状态。
				status = http.StatusInternalServerError
				span.RecordError(errors.New("handler panic"))
			}
			span.SetName(c.Request.Method + " " + route)
			span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
			if status >= http.StatusBadRequest {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			span.End()
			if recovered != nil {
				panic(recovered)
			}
		}()
		tid := span.SpanContext().TraceID().String()
		if !span.SpanContext().IsValid() {
			tid = c.GetHeader("X-Trace-Id")
			if tid == "" {
				tid = GenTraceID()
			}
		}
		if requestID := c.GetHeader("X-Trace-Id"); requestID != "" {
			span.SetAttributes(attribute.String("app.request_id", requestID))
		}
		c.Request = c.Request.WithContext(WithTrace(ctx, tid))
		c.Header("X-Trace-Id", tid)
		c.Next()
	}
}

// Metrics 记录请求计数与耗时。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		metrics.ObserveRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), start)
	}
}

// Recover 兜底 panic。
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "internal error", "trace_id": TraceFrom(c.Request.Context()),
				})
			}
		}()
		c.Next()
	}
}

// RateLimit 按 key（默认用户 id，兜底 IP）做令牌桶限流。
func (m *MW) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if u := auth.UserFrom(c.Request.Context()); u != nil {
			key = "u:" + filename(u.Username)
		}
		ok, err := m.rl.Allow(c.Request.Context(), key)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limiter unavailable"})
			return
		}
		if !ok {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

// Auth 校验 JWT 并注入当前用户到 context。
func (m *MW) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		raw := strings.TrimPrefix(h, "Bearer ")
		claims, err := auth.ParseToken(m.cfg.JWTSecret, raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		u, err := m.st.InvocationUser(c.Request.Context(), claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), u))
		trace.SpanFromContext(c.Request.Context()).SetAttributes(
			attribute.Int64("user.id", u.ID), attribute.String("user.role", u.Role),
		)
		c.Next()
	}
}

// RequirePermission 校验当前用户对指定资源执行某动作的权限。
// resourceType: server | tool。actionResourceID 支持 gin 参数名，如 "server_id"。
func (m *MW) RequirePermission(resourceType, action, idParam string) gin.HandlerFunc {
	rb := auth.NewRBAC(m.st)
	return func(c *gin.Context) {
		u := auth.UserFrom(c.Request.Context())
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}
		id := parseID(c.Param(idParam))
		if !rb.Can(c.Request.Context(), u, "role", resourceType, id, action) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}

// Audit 记录审计日志。调用方必须在完成后调用 c.Next()，此处 end 之后落库。
func (m *MW) Audit(action, resource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		u := auth.UserFrom(c.Request.Context())
		entry := &model.AuditLog{
			TraceID:    TraceFrom(c.Request.Context()),
			Action:     action,
			Resource:   resource,
			ResourceID: c.Param("id"),
			IP:         c.ClientIP(),
			UserAgent:  c.GetHeader("User-Agent"),
			Detail:     model.JSON{"method": c.Request.Method, "path": c.Request.URL.Path, "status": c.Writer.Status()},
		}
		if u != nil {
			entry.UserID = u.ID
		}
		m.audit.Submit(entry)
	}
}

func parseID(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

func filename(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == ' ' || b[i] == '/' || b[i] == '\\' {
			b[i] = '_'
		}
	}
	return string(b)
}
