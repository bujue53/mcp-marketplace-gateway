package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/config"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/gateway"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/middleware"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/registry"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/store"
)

// Server 持有 API 层依赖并路由到 gin。
type Server struct {
	cfg  *config.Config
	st   *store.Store
	reg  *registry.Service
	gw   *gateway.Service
	mw   *middleware.MW
	sink *store.CallSink
}

func New(cfg *config.Config, st *store.Store, reg *registry.Service, gw *gateway.Service, mw *middleware.MW) *Server {
	return &Server{cfg: cfg, st: st, reg: reg, gw: gw, mw: mw, sink: store.NewCallSink(st, 65536)}
}

// CloseSink 关闭异步调用日志（服务退出时调用）。
func (s *Server) CloseSink() { s.sink.Close() }

// Router 构建完整 gin 路由。
func (s *Server) Router() *gin.Engine {
	r := gin.New()
	r.Use(middleware.GatewayTiming())
	if s.cfg.RequestLogging {
		r.Use(gin.Logger())
	}
	r.Use(middleware.Recover(), middleware.CORS(), middleware.Trace(), middleware.Metrics())

	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := r.Group("/api")
	{
		api.POST("/auth/login", s.Login)

		servers := api.Group("/servers")
		servers.Use(s.mw.Auth(), s.mw.Audit("server.request", "server"))
		{
			servers.POST("", s.CreateServer)
			servers.GET("", s.ListServers)
			servers.GET("/events", s.ServerStatusEvents)
			servers.GET("/:id", s.GetServer)
			servers.GET("/:id/health", s.CheckServerHealth)
			servers.DELETE("/:id", s.DeleteServer)
			servers.POST("/generate", s.GenerateFromOpenAPI)
			servers.POST("/:id/sync", s.SyncServer)
		}

		mkt := api.Group("/marketplace")
		{
			mkt.GET("/tools", s.MarketplaceTools)
		}
		api.GET("/tools/:id", s.GetTool)
		api.GET("/tools/:id/versions", s.ToolVersions)
		api.GET("/tools/:id/comments", s.Comments)
		api.POST("/tools/:id/rate", s.mw.Auth(), s.mw.Audit("tool.rate", "tool"), s.RateTool)
		api.POST("/tools/:id/connect", s.mw.Auth(), s.mw.Audit("tool.connect", "tool"), s.ConnectTool)

		api.POST("/wrapper/skeleton", s.mw.Auth(), s.mw.Audit("wrapper.skeleton", "wrapper"), s.GenerateSkeleton)
		api.POST("/wrapper/skills", s.mw.Auth(), s.mw.Audit("wrapper.skills", "wrapper"), s.ConvertSkills)

		api.GET("/audit/alerts", s.mw.Auth(), s.AuditAlerts)
		api.GET("/audit/report", s.mw.Auth(), s.AuditReport)

		gwa := api.Group("/gateway")
		gwa.Use(s.mw.Auth(), s.mw.RateLimit(), s.mw.Audit("tool.invoke", "tool"))
		{
			gwa.POST("/invoke", s.InvokeTool)
		}

		api.GET("/stats/summary", s.StatsSummary)
		api.GET("/stats/daily", s.StatsDaily)
		api.GET("/stats/top", s.StatsTop)
	}

	// MCP Streamable HTTP 端点：Agent 走这里。
	r.POST("/mcp", s.mw.Auth(), s.mw.RateLimit(), s.mw.Audit("mcp.request", "mcp"), s.MCPEndpoint)
	r.OPTIONS("/mcp", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	return r
}
