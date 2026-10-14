-- 保存导入后的请求映射，网关重启后可重新装载；不保存调用凭据。
CREATE TABLE IF NOT EXISTS openapi_bindings (
    server_id BIGINT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
    bindings JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
