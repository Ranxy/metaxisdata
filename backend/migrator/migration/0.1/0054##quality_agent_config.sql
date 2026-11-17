-- Multi-agent runtime configuration. A quality_agent row keeps the agent's
-- identity (principal, token, display name); quality_agent_config carries the
-- runtime settings an administrator creates and edits in the Quality Agent
-- settings page. A config row is what makes an agent a built-in agent; agents
-- without one are external token principals only.
--
-- The data quality feature has never shipped, so this migration creates the
-- target shape directly and does not backfill any legacy rows.

CREATE TABLE IF NOT EXISTS quality_agent_config (
    agent_id INT4 PRIMARY KEY REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- 1 DRAFT, 2 ACTIVE, 3 PAUSED, 4 DISABLED (QualityAgentState).
    state INT2 NOT NULL DEFAULT 1,
    -- The owner is the only caller who may talk to the agent. A deleted owner
    -- leaves NULL; the notification falls back to workspace admins.
    owner_principal_id INT4 REFERENCES principal(id) ON DELETE SET NULL,
    -- JSON array of capability ids: "suggest", "adopt", "patrol", ...
    capabilities JSONB NOT NULL DEFAULT '[]',
    provider_profile TEXT NOT NULL DEFAULT '',
    -- 0 UNSPECIFIED, 1 OFF, 2 DAILY, 3 WEEKLY (QualityAgentReportFrequency).
    report_frequency INT2 NOT NULL DEFAULT 0,
    -- "HH:MM" in report_timezone.
    report_time TEXT NOT NULL DEFAULT '',
    report_timezone TEXT NOT NULL DEFAULT '',
    -- DayOfWeek for WEEKLY: 1 Monday .. 7 Sunday; 0 while unused.
    report_weekday INT2 NOT NULL DEFAULT 0,
    -- 0 OFF, 1 PAUSED, 2 ACTIVE (QualityAgentAdoptionMode). PAUSED creates an
    -- adopted check disabled for the owner to enable.
    adoption_mode INT2 NOT NULL DEFAULT 1,
    -- JSON array of QualityRuleType numbers eligible for auto-adoption; empty
    -- means the server default low-risk set.
    adoption_rule_types JSONB NOT NULL DEFAULT '[]',
    adoption_confidence_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.7,
    adoption_allow_critical BOOLEAN NOT NULL DEFAULT FALSE,
    adoption_max_per_day INT4 NOT NULL DEFAULT 10,
    conversation_messages_per_hour INT4 NOT NULL DEFAULT 60,
    -- Per-agent budgets; zero means the server default. The workspace hard caps
    -- still apply.
    tokens_per_hour INT8 NOT NULL DEFAULT 0,
    wakes_per_hour INT4 NOT NULL DEFAULT 0,
    max_concurrent_sessions INT4 NOT NULL DEFAULT 0,
    max_new_sessions_per_hour INT4 NOT NULL DEFAULT 0,
    max_active_sessions INT4 NOT NULL DEFAULT 0,
    last_active_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE quality_agent_config IS 'Per-agent runtime configuration for the built-in quality agent';
COMMENT ON COLUMN quality_agent_config.state IS 'Stored as QualityAgentState (QualityAgentState in the v1 proto)';
COMMENT ON COLUMN quality_agent_config.capabilities IS 'JSON array of capability ids; validated against the server capability catalog';
COMMENT ON COLUMN quality_agent_config.adoption_rule_types IS 'JSON array of QualityRuleType numbers; empty means the default low-risk set';

-- One row per database in one agent's scope. The same database may belong to
-- multiple agents; the settings page warns about the duplicate cost.
CREATE TABLE IF NOT EXISTS quality_agent_database (
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    database_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, database_ref)
);
CREATE INDEX IF NOT EXISTS idx_quality_agent_database_ref
    ON quality_agent_database(database_ref);
COMMENT ON TABLE quality_agent_database IS 'Database resource names in one quality agent''s scope';
COMMENT ON COLUMN quality_agent_database.database_ref IS 'instances/{instance}/databases/{database}';
