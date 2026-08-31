package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yafeng/mcp-marketplace/gateway/internal/api"
	"github.com/yafeng/mcp-marketplace/gateway/internal/auth"
	"github.com/yafeng/mcp-marketplace/gateway/internal/config"
	"github.com/yafeng/mcp-marketplace/gateway/internal/gateway"
	"github.com/yafeng/mcp-marketplace/gateway/internal/middleware"
	"github.com/yafeng/mcp-marketplace/gateway/internal/model"
	"github.com/yafeng/mcp-marketplace/gateway/internal/otel"
	"github.com/yafeng/mcp-marketplace/gateway/internal/registry"
	"github.com/yafeng/mcp-marketplace/gateway/internal/store"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		runServe(cfg)
	case "seed":
		if err := runSeed(cfg); err != nil {
			log.Fatalf("seed: %v", err)
		}
	case "login":
		if err := runLogin(cfg); err != nil {
			log.Fatalf("login: %v", err)
		}
	case "migrate":
		if err := runMigrate(cfg); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: ctl [serve|seed|login|migrate]\n")
		os.Exit(2)
	}
}

func runServe(cfg *config.Config) {
	ctx := context.Background()
	st, rdb := connect(ctx, cfg)

	shutdown, _ := otel.Init(ctx, "mcp-gateway", cfg.OTLPEndpoint)
	defer shutdown()

	ensureUsers(ctx, st, cfg)
	if cfg.SeedOnStartup {
		if err := seedMarket(ctx, cfg, st); err != nil {
			log.Printf("seed-on-startup skipped: %v", err)
		}
	}

	gw := gateway.NewService(st)
	reg := registry.New(st, gw)
	cb := middleware.NewCircuitBreaker(cfg.CircuitFailRatio, cfg.CircuitMinSamples, cfg.CircuitInterval)
	rl := middleware.NewRateLimiter(rdb, cfg.RateLimitRPS, cfg.RateLimitBurst)
	mw := middleware.New(cfg, st, rl, cb)
	srv := api.New(cfg, st, reg, gw, mw)
	defer srv.CloseSink()
	reg.StartHealthLoop(ctx, 30*time.Second)

	log.Printf("gateway listening on %s", cfg.HTTPAddr)
	if err := srv.Router().Run(cfg.HTTPAddr); err != nil {
		log.Fatalf("http: %v", err)
	}
}

func runSeed(cfg *config.Config) error {
	ctx := context.Background()
	st, _ := connect(ctx, cfg)
	ensureUsers(ctx, st, cfg)
	return seedMarket(ctx, cfg, st)
}

func runLogin(cfg *config.Config) error {
	ctx := context.Background()
	st, _ := connect(ctx, cfg)
	u, err := st.FindUserByUsername(ctx, cfg.AdminUser)
	if err != nil {
		ensureUsers(ctx, st, cfg)
		u, err = st.FindUserByUsername(ctx, cfg.AdminUser)
	}
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	token, exp, err := auth.IssueToken(cfg.JWTSecret, cfg.JWTTTL, u.ID, u.Username, u.Role)
	if err != nil {
		return err
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"token":      token,
		"expires_at": exp,
		"user":       map[string]any{"name": u.Username, "role": u.Role},
	})
	return nil
}

func runMigrate(cfg *config.Config) error {
	ctx := context.Background()
	pool, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return store.Migrate(ctx, pool, "migrations")
}

func connect(ctx context.Context, cfg *config.Config) (*store.Store, *redis.Client) {
	pool, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	if err := store.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	rdb, err := store.NewRedis(ctx, cfg.RedisAddr, cfg.RedisPass, cfg.RedisDB)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	return store.New(pool), rdb
}

func ensureUsers(ctx context.Context, st *store.Store, cfg *config.Config) {
	ensureUser(ctx, st, cfg.AdminUser, cfg.AdminPass, model.RoleAdmin)
	ensureUser(ctx, st, "dev", "dev123", model.RoleDeveloper)
}

func ensureUser(ctx context.Context, st *store.Store, name, pass, role string) {
	if _, err := st.FindUserByUsername(ctx, name); err == store.ErrNotFound {
		hash, _ := auth.HashPassword(pass)
		u := &model.User{Username: name, Email: name + "@mcp.local", PasswordHash: hash, Role: role}
		if err := st.CreateUser(ctx, u); err != nil {
			log.Printf("create user %s: %v", name, err)
		} else {
			log.Printf("created user %s (%s)", name, role)
		}
	}
}

// seedMarket 注册 mock 服务：legacy REST API（OpenAPI 自动包装）+ 原生 MCP Server。
func seedMarket(ctx context.Context, cfg *config.Config, st *store.Store) error {
	admin, err := st.FindUserByUsername(ctx, cfg.AdminUser)
	if err != nil {
		return err
	}
	gw := gateway.NewService(st)
	reg := registry.New(st, gw)

	spec, err := fetchSpec(ctx, cfg)
	if err != nil {
		return fmt.Errorf("fetch openapi spec: %w", err)
	}
	sv, tools, err := reg.CreateFromOpenAPI(ctx, spec, cfg.LegacyAPIURL, admin.ID)
	if err != nil {
		return fmt.Errorf("generate from openapi: %w", err)
	}
	log.Printf("registered server %q (id=%d) with %d generated tools", sv.Name, sv.ID, len(tools))

	mock := &model.MCPServer{
		Name: "mock-weather-mcp", Description: "原生 MCP 示例服务：天气工具", Version: "1.0.0",
		EndpointURL: cfg.MockMCPURL, Transport: "streamable_http", AuthType: "none",
		Status: "online", OwnerID: admin.ID, Tags: []string{"demo", "mcp"},
	}
	registerNative(ctx, reg, mock)

	registerNative(ctx, reg, &model.MCPServer{
		Name: "mock-file-server", Description: "原生 MCP 示例服务：文件工具", Version: "1.0.0",
		EndpointURL: cfg.MockFileURL, Transport: "streamable_http", AuthType: "none",
		Status: "online", OwnerID: admin.ID, Tags: []string{"demo", "file"},
	})
	registerNative(ctx, reg, &model.MCPServer{
		Name: "mock-db-server", Description: "原生 MCP 示例服务：数据库工具", Version: "1.0.0",
		EndpointURL: cfg.MockDBURL, Transport: "streamable_http", AuthType: "none",
		Status: "online", OwnerID: admin.ID, Tags: []string{"demo", "database"},
	})
	return nil
}

func registerNative(ctx context.Context, reg *registry.Service, sv *model.MCPServer) {
	if err := reg.RegisterServer(ctx, sv); err != nil {
		log.Printf("register %s: %v", sv.Name, err)
		return
	}
	if n, err := reg.SyncNativeServer(ctx, sv.ID); err != nil {
		log.Printf("sync %s tools: %v", sv.Name, err)
	} else {
		log.Printf("synced %d native tools from %s", n, sv.Name)
	}
}

func fetchSpec(ctx context.Context, cfg *config.Config) ([]byte, error) {
	url := cfg.OpenAPISpecURL
	if url == "" {
		url = cfg.LegacyAPIURL + "/openapi.json"
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
