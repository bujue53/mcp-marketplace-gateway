-- 企业级 MCP 工具市场与智能体网关 · 初始化

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    email         TEXT        NOT NULL DEFAULT '',
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'viewer',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mcp_servers (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT        NOT NULL UNIQUE,
    description  TEXT        NOT NULL DEFAULT '',
    version      TEXT        NOT NULL DEFAULT '1.0.0',
    endpoint_url TEXT        NOT NULL,
    transport    TEXT        NOT NULL DEFAULT 'streamable_http',
    auth_type    TEXT        NOT NULL DEFAULT 'none',
    status       TEXT        NOT NULL DEFAULT 'online',
    owner_id     BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    tags         TEXT[]      NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_status ON mcp_servers(status);

CREATE TABLE IF NOT EXISTS mcp_tools (
    id            BIGSERIAL PRIMARY KEY,
    server_id     BIGINT      NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    category      TEXT        NOT NULL DEFAULT 'general',
    tags          TEXT[]      NOT NULL DEFAULT '{}',
    input_schema  JSONB       NOT NULL DEFAULT '{}',
    output_schema JSONB       NOT NULL DEFAULT '{}',
    cost_model    JSONB       NOT NULL DEFAULT '{}',
    is_generated  BOOLEAN     NOT NULL DEFAULT false,
    version       TEXT        NOT NULL DEFAULT '1.0.0',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(server_id, name)
);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_server ON mcp_tools(server_id);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_category ON mcp_tools(category);

CREATE TABLE IF NOT EXISTS tool_versions (
    id         BIGSERIAL PRIMARY KEY,
    tool_id    BIGINT      NOT NULL REFERENCES mcp_tools(id) ON DELETE CASCADE,
    version    TEXT        NOT NULL,
    spec       JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tool_versions_tool ON tool_versions(tool_id);

CREATE TABLE IF NOT EXISTS tool_ratings (
    id         BIGSERIAL PRIMARY KEY,
    tool_id    BIGINT      NOT NULL REFERENCES mcp_tools(id) ON DELETE CASCADE,
    user_id    BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    rating     SMALLINT    NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tool_id, user_id)
);

-- RBAC 权限覆盖表：subject_type 为 role 或 user，resource_type 为 server 或 tool。
CREATE TABLE IF NOT EXISTS permissions (
    id            BIGSERIAL PRIMARY KEY,
    subject_type  TEXT    NOT NULL,
    subject_id    TEXT    NOT NULL,
    resource_type TEXT    NOT NULL,
    resource_id   BIGINT  NOT NULL,
    action        TEXT    NOT NULL,
    allowed       BOOLEAN NOT NULL DEFAULT true,
    UNIQUE(subject_type, subject_id, resource_type, resource_id, action)
);

CREATE TABLE IF NOT EXISTS tool_calls (
    id          BIGSERIAL PRIMARY KEY,
    trace_id    TEXT        NOT NULL DEFAULT '',
    user_id     BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    tool_id     BIGINT      REFERENCES mcp_tools(id) ON DELETE SET NULL,
    status_code INT         NOT NULL DEFAULT 0,
    latency_ms  BIGINT      NOT NULL DEFAULT 0,
    tokens_in   INT         NOT NULL DEFAULT 0,
    tokens_out  INT         NOT NULL DEFAULT 0,
    cost_micros BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_tool ON tool_calls(tool_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_created ON tool_calls(created_at);

CREATE TABLE IF NOT EXISTS audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    trace_id    TEXT        NOT NULL DEFAULT '',
    user_id     BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    action      TEXT        NOT NULL,
    resource    TEXT        NOT NULL,
    resource_id TEXT        NOT NULL DEFAULT '',
    detail      JSONB       NOT NULL DEFAULT '{}',
    ip          TEXT        NOT NULL DEFAULT '',
    user_agent  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_trace ON audit_logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);
