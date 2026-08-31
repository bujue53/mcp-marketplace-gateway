package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/yafeng/mcp-marketplace/gateway/internal/auth"
	"github.com/yafeng/mcp-marketplace/gateway/internal/config"
	"github.com/yafeng/mcp-marketplace/gateway/internal/metrics"
	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
	"github.com/yafeng/mcp-marketplace/gateway/internal/store"
)

// MW 聚合中间件所需的依赖。
type MW struct {
	cfg *config.Config
	st  *store.Store
	rl  *RateLimiter
	cb  *CircuitBreaker
}

func New(cfg *config.Config, st *store.Store, rl *RateLimiter, cb *CircuitBreaker) *MW {
	return &MW{cfg: cfg, st: st, rl: rl, cb: cb}
}

// CORS 允许前端跨域访问。
func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Trace-Id"},
		ExposeHeaders:    []string{"X-Trace-Id"},
		AllowCredentials: false,
	})
}

// Trace 生成/透传 trace id，并链路透传日志。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := c.GetHeader("X-Trace-Id")
		if tid == "" {
			tid = GenTraceID()
		}
		c.Request = c.Request.WithContext(WithTrace(c.Request.Context(), tid))
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
		u, err := m.st.FindUserByID(c.Request.Context(), claims.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), u))
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
			TraceID:   TraceFrom(c.Request.Context()),
			Action:    action,
			Resource:  resource,
			ResourceID: c.Param("id"),
			IP:        c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail:    model.JSON{"method": c.Request.Method, "path": c.Request.URL.Path},
		}
		if u != nil {
			entry.UserID = u.ID
		}
		_ = m.st.InsertAuditLog(context.Background(), entry)
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

