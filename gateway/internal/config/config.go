package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 聚合网关运行所需的全部配置，支持环境变量注入（Docker Compose / 本地）。
type Config struct {
	HTTPAddr string
	GinMode  string

	DatabaseURL string
	RedisAddr   string
	RedisPass   string
	RedisDB     int

	JWTSecret     string
	JWTTTL        time.Duration
	AdminUser     string
	AdminPass     string

	RateLimitRPS   int
	RateLimitBurst int

	CircuitFailRatio   float64
	CircuitMinSamples  int
	CircuitInterval    time.Duration
	CircuitWindow      time.Duration

	CostPer1KTokens float64

	OTLPEndpoint string
	OTLPProtocol string

	LegacyAPIURL string
	MockMCPURL   string
	MockFileURL  string
	MockDBURL    string
	OpenAPISpecURL string
	SeedOnStartup  bool
}

// FromEnv 从环境变量加载配置，未设置时应用默认值。
func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.HTTPAddr = ":" + envStr("HTTP_PORT", "8080")
	cfg.GinMode = envStr("GIN_MODE", "release")
	cfg.DatabaseURL = envStr("DATABASE_URL", "postgres://mcp:mcp-secret@localhost:5432/mcp_market?sslmode=disable")
	cfg.RedisAddr = envStr("REDIS_ADDR", "localhost:6379")
	cfg.RedisPass = envStr("REDIS_PASSWORD", "")
	cfg.RedisDB = envInt("REDIS_DB", 0)

	cfg.JWTSecret = envStr("JWT_SECRET", "change-me-in-prod-please-32-chars!")
	cfg.JWTTTL = time.Duration(envInt("JWT_TTL_HOURS", 24)) * time.Hour
	cfg.AdminUser = envStr("ADMIN_USER", "admin")
	cfg.AdminPass = envStr("ADMIN_PASS", "admin123")

	cfg.RateLimitRPS = envInt("RATE_LIMIT_RPS", 20)
	cfg.RateLimitBurst = envInt("RATE_LIMIT_BURST", 40)

	cfg.CircuitFailRatio = envFloat("CIRCUIT_FAIL_RATIO", 0.5)
	cfg.CircuitMinSamples = envInt("CIRCUIT_MIN_SAMPLES", 10)
	cfg.CircuitInterval = envDur("CIRCUIT_INTERVAL", 10*time.Second)
	cfg.CircuitWindow = envDur("CIRCUIT_WINDOW", 30*time.Second)

	cfg.CostPer1KTokens = envFloat("COST_PER_1K_TOKENS", 0.002)

	cfg.OTLPEndpoint = envStr("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	cfg.OTLPProtocol = envStr("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	cfg.LegacyAPIURL = envStr("LEGACY_API_URL", "http://localhost:9002")
	cfg.MockMCPURL = envStr("MOCK_MCP_URL", "http://localhost:9003")
	cfg.MockFileURL = envStr("MOCK_FILE_URL", "http://localhost:9004")
	cfg.MockDBURL = envStr("MOCK_DB_URL", "http://localhost:9005")
	cfg.OpenAPISpecURL = envStr("OPENAPI_SPEC_URL", "")
	cfg.SeedOnStartup = envBool("SEED_ON_STARTUP", false)

	if len(cfg.JWTSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET 至少需要 16 个字符")
	}
	return cfg, nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(os.Getenv(key))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return def
	}
	return def
}
