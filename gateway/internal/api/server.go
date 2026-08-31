package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yafeng/mcp-marketplace/gateway/internal/config"
	"github.com/yafeng/mcp-marketplace/gateway/internal/gateway"
	"github.com/yafeng/mcp-marketplace/gateway/internal/middleware"
	"github.com/yafeng/mcp-marketplace/gateway/internal/registry"
	"github.com/yafeng/mcp-marketplace/gateway/internal/store"
)

// Server 持有 API 层依赖并路由到 gin。
type Server struct {
	cfg *config.Config
	st  *store.Store
	reg *registry.Service
	gw  *gateway.Service
	mw  *middleware.MW
}

func New(cfg *config.Config, st *store.Store, reg *registry.Service, gw *gateway.Service, mw *middleware.MW) *Server {
	return &Server{cfg: cfg, st: st, reg: reg, gw: gw, mw: mw}
}

// Router 构建完整 gin 路由。
func (s *Server) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), middleware.Recover(), middleware.CORS(), middleware.Trace(), middleware.Metrics())

	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := r.Group("/api")
	{
		api.POST("/auth/login", s.Login)

		servers := api.Group("/servers")
		servers.Use(s.mw.Auth())
		{
			servers.POST("", s.CreateServer)
			servers.GET("", s.ListServers)
			servers.GET("/:id", s.GetServer)
			servers.DELETE("/:id", s.DeleteServer)
			servers.POST("/generate", s.GenerateFromOpenAPI)
			servers.POST("/:id/sync", s.SyncServer)
		}

		mkt := api.Group("/marketplace")
		{
			mkt.GET("/tools", s.MarketplaceTools)
		}
		api.GET("/tools/:id", s.GetTool)
		api.POST("/tools/:id/rate", s.mw.Auth(), s.RateTool)
		api.POST("/tools/:id/connect", s.mw.Auth(), s.ConnectTool)

		gwa := api.Group("/gateway")
		gwa.Use(s.mw.Auth(), s.mw.RateLimit())
		{
			gwa.POST("/invoke", s.InvokeTool)
		}

		api.GET("/stats/summary", s.StatsSummary)
		api.GET("/stats/daily", s.StatsDaily)
		api.GET("/stats/top", s.StatsTop)
	}

	// MCP Streamable HTTP 端点：Agent 走这里。
	r.POST("/mcp", s.mw.Auth(), s.mw.RateLimit(), s.MCPEndpoint)
	r.OPTIONS("/mcp", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	return r
}

