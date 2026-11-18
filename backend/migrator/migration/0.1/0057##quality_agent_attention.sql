-- A quality agent's escalation: something needs its owner. The notification is the
-- doorbell; this table is the durable to-do item that survives the notification and
-- can be answered from the agent home.
CREATE TABLE IF NOT EXISTS quality_agent_attention (
    id BIGSERIAL PRIMARY KEY,
    -- workspaces/{workspace}/qualityAgentAttentions/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- 1 BLOCKED, 2 PROPOSAL_PENDING, 3 REPORT_ANOMALY (QualityAgentAttentionKind).
    kind INT2 NOT NULL DEFAULT 1,
    -- The target session this escalation came from; NULL for agent-level events.
    session_id INT8 REFERENCES quality_agent_session(id) ON DELETE SET NULL,
    -- A resource the owner should open, e.g. the proposal or the report.
    reference TEXT COLLATE "C" NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    -- 1 OPEN, 2 RESOLVED, 3 DISMISSED (QualityAgentAttentionStatus).
    status INT2 NOT NULL DEFAULT 1,
    -- Idempotency: one open escalation per agent/kind/reference. A retried runner
    -- pass or a re-blocked session cannot page the owner twice.
    dedupe_key TEXT NOT NULL DEFAULT '',
    resolved_by INT4,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_attention_resource
    ON quality_agent_attention(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_attention_open_dedupe
    ON quality_agent_attention(agent_id, dedupe_key) WHERE status = 1;
CREATE INDEX IF NOT EXISTS idx_quality_agent_attention_agent_status
    ON quality_agent_attention(agent_id, status, created_at DESC);
COMMENT ON TABLE quality_agent_attention IS 'Per-agent escalation to the owner; the durable to-do behind a notification';
