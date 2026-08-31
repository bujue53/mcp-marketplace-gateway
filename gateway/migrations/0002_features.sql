-- 对照选题文档补充字段/表

ALTER TABLE tool_calls  ADD COLUMN IF NOT EXISTS argument_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_tools   ADD COLUMN IF NOT EXISTS sensitive BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS healthy BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS last_health_at TIMESTAMPTZ;
ALTER TABLE tool_ratings ADD COLUMN IF NOT EXISTS comment TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS audit_alerts (
    id         BIGSERIAL PRIMARY KEY,
    severity   TEXT        NOT NULL DEFAULT 'warning',
    rule       TEXT        NOT NULL,
    tool_id    BIGINT      REFERENCES mcp_tools(id) ON DELETE SET NULL,
    detail     JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_alerts_created ON audit_alerts(created_at);
