package model

import "time"

// Role / 权限常量。
const (
	RoleAdmin     = "admin"
	RoleDeveloper = "developer"
	RoleViewer    = "viewer"
)

// 资源动作。
const (
	ActionInvoke = "invoke"
	ActionManage = "manage"
	ActionRead   = "read"
)

// User 平台用户。
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// MCPServer 注册中心里的一个 MCP Server。
type MCPServer struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	EndpointURL string    `json:"endpoint_url"`
	Transport   string    `json:"transport"` // streamable_http | sse
	AuthType    string    `json:"auth_type"`  // none | bearer | oauth2
	Status      string    `json:"status"`     // online | offline
	Healthy     bool      `json:"healthy"`
	LastHealthAt *time.Time `json:"last_health_at,omitempty"`
	OwnerID     int64     `json:"owner_id"`
	Tags        []string  `json:"tags"`       // 数据库存 text[] 映射为 []string
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MCPTool 一个可被 Agent 调用的工具定义。
type MCPTool struct {
	ID           int64           `json:"id"`
	ServerID     int64           `json:"server_id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	Tags         []string        `json:"tags"`
	InputSchema  JSON            `json:"input_schema"`
	OutputSchema JSON            `json:"output_schema"`
	CostModel    JSON            `json:"cost_model"`
	IsGenerated  bool            `json:"is_generated"`
	Sensitive    bool            `json:"sensitive"`
	Version      string          `json:"version"`
	Rating       float64         `json:"rating"`
	RatingCount  int64           `json:"rating_count"`
	CallCount    int64           `json:"call_count"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ToolVersion 工具版本快照。
type ToolVersion struct {
	ID        int64     `json:"id"`
	ToolID    int64     `json:"tool_id"`
	Version   string    `json:"version"`
	Spec      JSON      `json:"spec"`
	CreatedAt time.Time `json:"created_at"`
}

// ToolCall 一次网关调用记录（用于统计与成本计量）。
type ToolCall struct {
	ID         int64     `json:"id"`
	TraceID    string    `json:"trace_id"`
	UserID     int64     `json:"user_id"`
	ToolID     int64     `json:"tool_id"`
	StatusCode int       `json:"status_code"`
	LatencyMs  int64     `json:"latency_ms"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
	CostMicros int64     `json:"cost_micros"`
	ArgumentSummary string `json:"argument_summary"`
	CreatedAt  time.Time `json:"created_at"`
}

// ToolComment 评分评论。
type ToolComment struct {
	Rating   int    `json:"rating"`
	Comment  string `json:"comment"`
	Username string `json:"username"`
}

// AuditAlert 审计告警。
type AuditAlert struct {
	ID        int64     `json:"id"`
	Severity  string    `json:"severity"`
	Rule      string    `json:"rule"`
	ToolID    int64     `json:"tool_id,omitempty"`
	Detail    JSON      `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// SensitiveHot 敏感工具高频调用（用于告警）。
type SensitiveHot struct {
	ToolID int64  `json:"tool_id"`
	Name   string `json:"name"`
	Calls  int64  `json:"calls"`
}

// AuditLog 全链路审计日志。
type AuditLog struct {
	ID         int64     `json:"id"`
	TraceID    string    `json:"trace_id"`
	UserID     int64     `json:"user_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Detail     JSON      `json:"detail"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
}

// KeyValue 通用小对象（内存/审计使用）。
type KeyValue map[string]any
