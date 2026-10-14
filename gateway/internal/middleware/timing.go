package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/metrics"
)

// GatewayTiming 在响应 trailer 中记录本次请求总处理时间减去下游 HTTP 读完时间。
func GatewayTiming() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		ctx, timing := metrics.WithTiming(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		c.Header("Trailer", "X-Gateway-Overhead-Ms")
		c.Next()
		overhead := time.Since(start) - time.Duration(timing.DownstreamNS.Load())
		if overhead < 0 {
			overhead = 0
		}
		c.Header("X-Gateway-Overhead-Ms", strconv.FormatFloat(float64(overhead)/float64(time.Millisecond), 'f', 4, 64))
	}
}
