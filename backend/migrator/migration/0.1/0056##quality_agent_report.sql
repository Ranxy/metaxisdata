-- Per-agent patrol reports. A report is a deterministic snapshot over the agent's
-- database scope for one period; the body is rendered markdown so the report can be
-- read in the conversation and copied out, while the columns keep the counts
-- queryable without parsing the body.
CREATE TABLE IF NOT EXISTS quality_agent_report (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentReports/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- 1 SCHEDULED, 2 ON_DEMAND (QualityAgentReportTrigger).
    trigger INT2 NOT NULL DEFAULT 1,
    -- 1 DAILY, 2 WEEKLY, 3 ON_DEMAND (QualityAgentReportPeriod).
    period_kind INT2 NOT NULL DEFAULT 1,
    period_key TEXT NOT NULL DEFAULT '',
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    -- 1 PENDING, 2 READY, 3 FAILED (QualityAgentReportState).
    state INT2 NOT NULL DEFAULT 1,
    -- Database resource names the report covers.
    scope TEXT[] NOT NULL DEFAULT '{}',
    target_count INT4 NOT NULL DEFAULT 0,
    covered_target_count INT4 NOT NULL DEFAULT 0,
    rule_count INT4 NOT NULL DEFAULT 0,
    execution_count INT4 NOT NULL DEFAULT 0,
    passed_count INT4 NOT NULL DEFAULT 0,
    warned_count INT4 NOT NULL DEFAULT 0,
    failed_count INT4 NOT NULL DEFAULT 0,
    errored_count INT4 NOT NULL DEFAULT 0,
    new_issue_count INT4 NOT NULL DEFAULT 0,
    open_issue_count INT4 NOT NULL DEFAULT 0,
    pending_proposal_count INT4 NOT NULL DEFAULT 0,
    tokens_used INT8 NOT NULL DEFAULT 0,
    severity INT2 NOT NULL DEFAULT 0,
    body_markdown TEXT NOT NULL DEFAULT '',
    failure_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    generated_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_report_resource
    ON quality_agent_report(resource_id);
-- The period identity is (agent, period). The column list matches the final schema,
-- where reports are always scheduled and there is no trigger column; 0062 rebuilds this
-- table into that shape.
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_report_period
    ON quality_agent_report(agent_id, period_key);
CREATE INDEX IF NOT EXISTS idx_quality_agent_report_agent_created
    ON quality_agent_report(agent_id, created_at DESC);
COMMENT ON TABLE quality_agent_report IS 'Per-agent deterministic patrol report over the agent database scope';
COMMENT ON COLUMN quality_agent_report.scope IS 'Database resource names the report covers';
COMMENT ON COLUMN quality_agent_report.body_markdown IS 'Rendered report body; counts are also stored as columns';
