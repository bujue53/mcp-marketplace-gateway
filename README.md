# 企业级 MCP 工具市场与智能体网关（题目 32）

> 学校 4 周软件工程项目实训 · 选题 32 · 面向 **AI 应用开发 / 平台工程** 方向

本项目构建一个企业级 **MCP（Model Context Protocol）工具市场与智能体网关**，统一管理企业内部 MCP Server 的注册、发现、版本、权限与审计，并通过网关层完成**鉴权、限流熔断、成本计量与全链路审计**。皇冠亮点是把遗留 REST API 的 **OpenAPI 3.x 规范自动包装成可被 Agent 调用的 MCP 工具**，解决企业「工具孤岛」与 Agent 治理难题。

## 一、系统架构

```text
                       ┌────────────────────────────────────────────┐
   Agent / Test Client │  MCP Streamable HTTP + JWT · RBAC          │
   ───────────────────▶│                                            │
                       │          ┌──────────────────────────────┐  │
                       │          │   Agent Gateway (Go + Gin)   │  │
                       │          │  鉴权 · 限流 · 熔断 · 成本 · 审计 │  │
                       │          └───────┬──────────────┬───────┘  │
                       │   Registry API   │   Tool 转发    │  MCP 端点 │
                       └──────────────────┼──────────────┼──────────┘
                                          │              │
                              ┌───────────▼───┐   ┌───────▼────────┐
                              │ PostgreSQL    │   │ Redis          │
                              │ 注册·版本·权限  │   │ 限流·熔断·缓存   │
                              │ 评分·调用·成本  │   └────────────────┘
                              └───────────────┘
                                          │
              ┌───────────────────────────┼───────────────────────────┐
              ▼                           ▼                           ▼
      ┌───────────────┐          ┌────────────────┐          ┌────────────────┐
      │ mock-mcp-server│          │ mock-mcp-server│          │  legacy REST API │
      │ (原生 MCP 工具) │          │  (原生MCP工具)  │          │ + OpenAPI 3.x │
      └───────────────┘          └────────────────┘          └────────────────┘
                                                                    ▲
                                    OpenAPI → MCP 自动包装器 ◀──────┘
```

## 二、技术栈

| 模块 | 技术 |
| --- | --- |
| Agent 网关 | Go + Gin，MCP Streamable HTTP |
| 注册中心 / 元数据 | PostgreSQL（pgx） |
| 缓存 / 限流 / 熔断 | Redis（go-redis） |
| 认证 / 授权 | JWT（golang-jwt）+ RBAC |
| 可观测 | OpenTelemetry + Jaeger + Prometheus |
| 前端 | React 19 + TypeScript + TailwindCSS + Vite |
| 部署 | Docker Compose（PostgreSQL + Redis + gateway + web + mock 服务） |

## 三、仓库结构

```text
mcp-marketplace/
├── gateway/            # Go 网关（注册中心 + 网关 + MCP + OpenAPI 包装器）
│   ├── cmd/            # 入口
│   ├── internal/       # 业务与基础设施
│   │   ├── api/        # HTTP 路由与处理器
│   │   ├── auth/       # JWT / RBAC
│   │   ├── middleware/ # 鉴权 / 限流 / 熔断 / 成本 / 审计 / 追踪
│   │   ├── registry/   # MCP Server 注册 / 发现 / 版本
│   │   ├── gateway/    # 调用转发 / 工具池
│   │   ├── wrapper/    # OpenAPI → MCP 工具生成器
│   │   ├── mcp/        # MCP Streamable HTTP 协议层
│   │   ├── cost/       # 成本计量
│   │   ├── audit/      # 审计日志
│   │   ├── store/      # PostgreSQL / Redis 客户端与查询
│   │   └── config/     # 配置
│   └── migrations/     # SQL 迁移
├── web/                # React 19 前端（工具市场）
├── servers/            # mock MCP Server 与遗留 REST API
├── specs/              # OpenAPI 3.x 示例规范（包装器输入）
├── agents/             # Agent / 测试客户端
├── docs/               # 开发报告 + 部署文档
├── docker-compose.yml
├── Makefile
└── .env.example
```

## 四、快速开始

前置：Docker 桌面版 + Docker Compose。

```bash
cp .env.example .env            # 修改 JWT/DB 密码等
docker compose up --build -d    # 一键拉起 all-in-one
docker compose ps               # 查看服务状态
```

服务端口：

| 服务 | 地址 |
| --- | --- |
| 网关 API | http://localhost:8080 |
| 前端市场页 | http://localhost:5173 |
| Jaeger | http://localhost:16686 |
| Prometheus | http://localhost:9090 |

浏览器打开前端市场页即可浏览 / 搜索 / 评分 / 一键接入 MCP 工具。

## 五、端到端演示（Agent 通过网关调用自动包装的 MCP 工具）

```bash
# 1. 登录拿 Token
docker compose exec gateway sh -c './bin/ctl login --user admin --pass admin123'
# 2. 用 Agent 客户端调用：MCP initialize → tools/list → tools/call
python agents/mcp_client.py --base http://localhost:8080/mcp \
  --api-key <你的token> --tool weather:current --args '{"city":"东莞"}'
```

该调用会经过：**JWT 鉴权 → RBAC 授权 → Redis 限流 → 熔断判断 → 成本计量 → 全链路审计日志**，最终命中由 OpenAPI 自动包装的 `legacy-weather-api` 工具。

## 六、已落地 / 待办

**已落地**

- MCP Server 注册中心（注册 / 发现 / 版本 / 权限 / 审计）
- 工具市场 API 与前端页（浏览 / 搜索 / 评分 / 一键接入 / 调用统计）
- Agent 网关（JWT + RBAC + 限流 + 熔断 + 成本计量 + 审计日志）
- OpenAPI 3.x → MCP 工具自动生成器（核心亮点）
- Docker Compose 一键部署 + mock 服务

**待办扩展（加分项）**

- Skills → MCP 适配器
- ClickHouse 调用日志分析
- 分布式高可用与一致性
- 网关侧自动包装器 UI（在市场上「生成工具」）

## 七、开发报告摘要

详见 [docs/开发报告.md](docs/开发报告.md)（架构设计、模块划分、核心流程、包装器设计、限流/成本算法）。

