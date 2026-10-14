package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/auth"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/gateway"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/mcp"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/metrics"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/middleware"
	"github.com/bujue53/mcp-marketplace-gateway/gateway/internal/model"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type invokeReq struct {
	ToolID    int64           `json:"tool_id"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
}

// InvokeTool REST 风格网关调用入口。
func (s *Server) InvokeTool(c *gin.Context) {
	u := auth.UserFrom(c.Request.Context())
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req invokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	tool := s.lookupTool(c.Request.Context(), req.ToolID, req.ToolName)
	if tool == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
		return
	}
	if !s.canInvoke(c.Request.Context(), u, tool) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: permission denied"})
		return
	}

	start := time.Now()
	call := &model.ToolCall{
		TraceID:         middleware.TraceFrom(c.Request.Context()),
		UserID:          u.ID,
		ToolID:          tool.ID,
		Sensitive:       tool.Sensitive,
		TokensIn:        gateway.EstimateTokens(req.Arguments),
		TokensOut:       0,
		ArgumentSummary: summarizeArgs(req.Arguments),
	}
	defer observeCallSpan(c.Request.Context(), call)
	if !s.mw.AllowToolCall(tool.ID) {
		call.StatusCode = http.StatusServiceUnavailable
		s.sink.Submit(call)
		metrics.ObserveToolCall(gateway.Slug(tool), http.StatusServiceUnavailable)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "circuit breaker open", "tool": tool.Name})
		return
	}
	result, err := s.gw.Invoke(c.Request.Context(), tool, req.Arguments)
	s.mw.RecordToolCall(tool.ID, err == nil && result != nil && !result.IsError)
	call.LatencyMs = time.Since(start).Milliseconds()
	call.StatusCode = 200
	if err != nil {
		call.StatusCode = 500
		s.sink.Submit(call)
		metrics.ObserveToolCall(gateway.Slug(tool), 500)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	call.TokensOut = estimateOut(result)
	call.CostMicros = computeCost(call.TokensIn+call.TokensOut, s.cfg.CostPer1KTokens)
	if result != nil && result.IsError {
		call.StatusCode = http.StatusBadGateway
		trace.SpanFromContext(c.Request.Context()).SetStatus(codes.Error, "tool returned an error")
	}
	s.sink.Submit(call)
	metrics.ObserveToolCall(gateway.Slug(tool), call.StatusCode)
	c.JSON(http.StatusOK, gin.H{
		"trace_id":    call.TraceID,
		"tool":        tool.Name,
		"cost_micros": call.CostMicros,
		"latency_ms":  call.LatencyMs,
		"result":      result,
	})
}

// MCPEndpoint Agent 走 MCP Streamable HTTP 调用网关端点。
func (s *Server) MCPEndpoint(c *gin.Context) {
	mcp.New("mcp-gateway", "1.0.0", &mcpHandler{s: s}).Route(c)
}

// mcpHandler 把网关注册的工具体现为一个 MCP 服务，供 Agent 通过 /mcp 发现与调用。
type mcpHandler struct {
	s *Server
}

func (h *mcpHandler) ListTools() ([]mcp.Tool, error) {
	return h.ListToolsContext(context.Background())
}

func (h *mcpHandler) ListToolsContext(ctx context.Context) ([]mcp.Tool, error) {
	u := auth.UserFrom(ctx)
	if u == nil {
		return []mcp.Tool{}, nil
	}
	items, err := h.s.st.ListTools(ctx, "", "", 200)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.Tool, 0, len(items))
	for _, t := range items {
		if !h.s.canInvoke(ctx, u, t) {
			continue
		}
		out = append(out, mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: toRaw(t.InputSchema)})
	}
	return out, nil
}

func (h *mcpHandler) CallTool(ctx context.Context, name string, args []byte) (*mcp.CallToolResult, error) {
	tool, err := h.s.st.InvocationTool(ctx, 0, name)
	if err != nil {
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "tool not found"}}, IsError: true}, nil
	}
	u := auth.UserFrom(ctx)
	if u == nil {
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "unauthenticated"}}, IsError: true}, nil
	}
	if !h.s.canInvoke(ctx, u, tool) {
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "forbidden"}}, IsError: true}, nil
	}
	start := time.Now()
	call := &model.ToolCall{
		TraceID:         middleware.TraceFrom(ctx),
		UserID:          u.ID,
		ToolID:          tool.ID,
		Sensitive:       tool.Sensitive,
		TokensIn:        gateway.EstimateTokens(args),
		ArgumentSummary: summarizeArgs(args),
	}
	defer observeCallSpan(ctx, call)
	if !h.s.mw.AllowToolCall(tool.ID) {
		call.StatusCode = http.StatusServiceUnavailable
		h.s.sink.Submit(call)
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "circuit breaker open"}}, IsError: true}, nil
	}
	result, err := h.s.gw.Invoke(ctx, tool, args)
	h.s.mw.RecordToolCall(tool.ID, err == nil && result != nil && !result.IsError)
	call.LatencyMs = time.Since(start).Milliseconds()
	call.StatusCode = 200
	if err != nil {
		call.StatusCode = 500
		h.s.sink.Submit(call)
		return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	call.TokensOut = estimateOut(result)
	call.CostMicros = computeCost(call.TokensIn+call.TokensOut, h.s.cfg.CostPer1KTokens)
	if result != nil && result.IsError {
		call.StatusCode = http.StatusBadGateway
	}
	h.s.sink.Submit(call)
	return result, nil
}

// ---- stats ----

func (s *Server) StatsSummary(c *gin.Context) {
	summary, err := s.st.StatsSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("X-Analytics-Source", "postgres")
	if s.st.Analytics != nil {
		if analytics, err := s.st.Analytics.Summary(c.Request.Context(), false); err == nil {
			summary["calls"] = analytics["calls"]
			summary["cost_micros"] = analytics["cost_micros"]
			summary["errors"] = analytics["errors"]
			summary["p99_latency_ms"] = analytics["p99_latency_ms"]
			c.Header("X-Analytics-Source", "clickhouse")
		}
	}
	c.JSON(http.StatusOK, summary)
}

func (s *Server) StatsDaily(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 365 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be 1-365"})
		return
	}
	if s.st.Analytics != nil {
		if data, err := s.st.Analytics.Daily(c.Request.Context(), days); err == nil {
			c.Header("X-Analytics-Source", "clickhouse")
			c.JSON(http.StatusOK, gin.H{"daily": data})
			return
		}
	}
	c.Header("X-Analytics-Source", "postgres")
	data, err := s.st.DailyCalls(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"daily": data})
}

func (s *Server) StatsTop(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be 1-100"})
		return
	}
	if s.st.Analytics != nil {
		if rows, err := s.st.Analytics.Top(c.Request.Context(), limit); err == nil {
			items := []*model.MCPTool{}
			for _, row := range rows {
				id := int64(row["tool_id"].(float64))
				if tool, err := s.st.GetToolByID(c.Request.Context(), id); err == nil {
					tool.CallCount = int64(row["call_count"].(float64))
					items = append(items, tool)
				}
			}
			c.Header("X-Analytics-Source", "clickhouse")
			c.JSON(http.StatusOK, gin.H{"items": items})
			return
		}
	}
	c.Header("X-Analytics-Source", "postgres")
	items, err := s.st.TopToolsByCalls(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---- helpers ----

func observeCallSpan(ctx context.Context, call *model.ToolCall) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int64("tool.id", call.ToolID),
		attribute.Int("tool.tokens_in", call.TokensIn), attribute.Int("tool.tokens_out", call.TokensOut),
		attribute.Int64("tool.cost_micros", call.CostMicros), attribute.Int("tool.status_code", call.StatusCode))
	if call.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, "tool call rejected or failed")
	}
}

func (s *Server) lookupTool(ctx context.Context, id int64, name string) *model.MCPTool {
	if id > 0 {
		if t, err := s.st.InvocationTool(ctx, id, ""); err == nil {
			return t
		}
	}
	if name != "" {
		if t, err := s.st.InvocationTool(ctx, 0, name); err == nil {
			return t
		}
	}
	return nil
}

func (s *Server) canInvoke(ctx context.Context, u *model.User, tool *model.MCPTool) bool {
	return auth.NewRBAC(s.st).Can(ctx, u, "user", "tool", tool.ID, "invoke")
}

func computeCost(tokens int, ratePer1K float64) int64 {
	return int64(float64(tokens) * ratePer1K * 1000)
}

func estimateOut(result *mcp.CallToolResult) int {
	if result == nil {
		return 0
	}
	n := 0
	for _, b := range result.Content {
		n += len(b.Text) / 4
	}
	return n
}

func toRaw(m model.JSON) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

// summarizeArgs 把调用参数截断为可读的摘要，用于审计展示。
func summarizeArgs(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) == nil && args != nil {
		for _, key := range []string{"auth", "headers", "cookies"} {
			if _, exists := args[key]; exists {
				args[key] = "[redacted]"
			}
		}
		if encoded, err := json.Marshal(args); err == nil {
			raw = encoded
		}
	}
	s := strings.ToValidUTF8(string(raw), "\uFFFD")
	if len(s) > 256 {
		end := 256
		for end > 0 && !utf8.ValidString(s[:end]) {
			end--
		}
		return strings.Clone(s[:end])
	}
	return s
}
