-- Pre-release rebuild of the quality agent schema.
--
-- The quality agent has never shipped, so this migration rebuilds every agent table to
-- its final shape and drops the removed change-request, investigation and config tables.
-- Existing development rows in these tables are discarded: pre-release rebuild, data
-- loss accepted. There are no backfills.
--
-- The same final shape is what migration/LATEST.sql creates for a fresh install, so this
-- file is idempotent when applied on top of LATEST: every drop is IF EXISTS and every
-- create is IF NOT EXISTS.

-- Suggestions that named an agent lose the pointer first: the agent ids are rebuilt, so
-- the old ids are meaningless. The foreign key is re-added after the rebuild.
ALTER TABLE quality_suggestion ADD COLUMN IF NOT EXISTS agent_id INT4;
ALTER TABLE quality_suggestion DROP CONSTRAINT IF EXISTS quality_suggestion_agent_id_fkey;
UPDATE quality_suggestion SET agent_id = NULL WHERE agent_id IS NOT NULL;

-- The agent tables reference each other and quality_agent, so drop the children first.
-- A later migration that adds a child of quality_agent belongs in this list too: this
-- file is replayed on top of LATEST, where the child exists.
DROP TABLE IF EXISTS quality_agent_case_action;
DROP TABLE IF EXISTS quality_agent_case;
DROP TABLE IF EXISTS quality_agent_request;
DROP TABLE IF EXISTS quality_agent_ask;
DROP TABLE IF EXISTS quality_agent_attention;
DROP TABLE IF EXISTS quality_agent_report;
DROP TABLE IF EXISTS quality_agent_adoption;
DROP TABLE IF EXISTS quality_agent_enrollment;
DROP TABLE IF EXISTS quality_agent_message;
DROP TABLE IF EXISTS quality_agent_session;
DROP TABLE IF EXISTS quality_agent_database;
DROP TABLE IF EXISTS quality_agent_config;
DROP TABLE IF EXISTS quality_change_request;
DROP TABLE IF EXISTS quality_investigation;
DROP TABLE IF EXISTS quality_agent;
DROP TABLE IF EXISTS quality_memory;

-- A quality agent is one built-in agent: an internal principal (its IAM and audit
-- actor) plus the runtime configuration an administrator edits. State is the only
-- lifecycle truth; there is no separate enabled flag and no config side table.
CREATE TABLE IF NOT EXISTS quality_agent (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    principal_id INT4 NOT NULL UNIQUE REFERENCES principal(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- 1 DRAFT, 2 ACTIVE, 3 PAUSED, 4 DISABLED (QualityAgentState); DISABLED is terminal.
    state INT2 NOT NULL DEFAULT 1,
    -- The owner is the only human who may talk to the agent. A deleted owner leaves
    -- NULL; the notification falls back to workspace admins.
    owner_principal_id INT4 REFERENCES principal(id) ON DELETE SET NULL,
    -- JSON array of capability ids: "suggest", "adopt", "patrol", ...
    capabilities JSONB NOT NULL DEFAULT '[]',
    -- 0 UNSPECIFIED, 1 OFF, 2 DAILY, 3 WEEKLY (QualityAgentReportFrequency).
    report_frequency INT2 NOT NULL DEFAULT 0,
    -- "HH:MM" in report_timezone.
    report_time TEXT NOT NULL DEFAULT '',
    report_timezone TEXT NOT NULL DEFAULT '',
    -- DayOfWeek for WEEKLY: 1 Monday .. 7 Sunday; 0 while unused.
    report_weekday INT2 NOT NULL DEFAULT 0,
    -- 0 UNSPECIFIED, 1 OFF, 2 PAUSED, 3 ACTIVE (QualityAgentAdoptionMode). PAUSED
    -- creates an adopted check disabled for the owner to enable.
    adoption_mode INT2 NOT NULL DEFAULT 2,
    -- JSON array of QualityRuleType numbers eligible for auto-adoption; empty means
    -- the server default low-risk set.
    adoption_rule_types JSONB NOT NULL DEFAULT '[]',
    adoption_confidence_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.7,
    adoption_allow_critical BOOLEAN NOT NULL DEFAULT FALSE,
    adoption_max_per_day INT4 NOT NULL DEFAULT 10,
    conversation_messages_per_hour INT4 NOT NULL DEFAULT 60,
    -- Per-agent budgets; zero means the server default. The server hard caps still
    -- apply.
    tokens_per_hour INT8 NOT NULL DEFAULT 0,
    wakes_per_hour INT4 NOT NULL DEFAULT 0,
    max_concurrent_sessions INT4 NOT NULL DEFAULT 0,
    max_new_sessions_per_hour INT4 NOT NULL DEFAULT 0,
    max_active_sessions INT4 NOT NULL DEFAULT 0,
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_resource
    ON quality_agent(resource_id);
COMMENT ON TABLE quality_agent IS 'Built-in quality agents: the internal principal plus its runtime configuration';
COMMENT ON COLUMN quality_agent.principal_id IS 'The internal principal the agent acts as (type SERVICE_ACCOUNT); never an API caller';
COMMENT ON COLUMN quality_agent.state IS 'Stored as QualityAgentState (v1 QualityAgentState); the only lifecycle truth';
COMMENT ON COLUMN quality_agent.capabilities IS 'JSON array of capability ids; validated against the server capability catalog';
COMMENT ON COLUMN quality_agent.adoption_rule_types IS 'JSON array of QualityRuleType numbers; empty means the default low-risk set';
ALTER SEQUENCE quality_agent_id_seq RESTART WITH 101;

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

-- An asset fact is what the agent knows deterministically about one target. A fact is
-- written confirmed; a newer fact supersedes the previous one of the same subject, and
-- an expired fact is reported expired on read rather than deleted.
CREATE TABLE IF NOT EXISTS quality_memory (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    -- Stored as QualityMemoryKind (proto/store/store/data_quality.proto); only ASSET_FACT is written.
    kind INT2 NOT NULL DEFAULT 1,
    subject TEXT NOT NULL DEFAULT '',
    statement TEXT NOT NULL DEFAULT '',
    -- Stored as QualityMemorySource (proto/store/store/data_quality.proto); agent facts are AGENT.
    source INT2 NOT NULL DEFAULT 1,
    -- Stored as QualityMemoryStatus (proto/store/store/data_quality.proto); CONFIRMED, then SUPERSEDED.
    status INT2 NOT NULL DEFAULT 2,
    -- Stored as QualityMemoryEvidenceList (proto/store/store/data_quality.proto); references only, never sample payloads
    evidence JSONB NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ,
    superseded_by TEXT NOT NULL DEFAULT '',
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_memory_resource
    ON quality_memory(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_memory_subject
    ON quality_memory(subject, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_memory_status
    ON quality_memory(status, created_at DESC);
COMMENT ON TABLE quality_memory IS 'Confirmed asset facts kept across wakes; superseded and expired rows stay for the correction path';
COMMENT ON COLUMN quality_memory.kind IS 'Stored as QualityMemoryKind (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.source IS 'Stored as QualityMemorySource (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.status IS 'Stored as QualityMemoryStatus (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.evidence IS 'Stored as QualityMemoryEvidenceList (proto/store/store/data_quality.proto); references only, never sample payloads';
ALTER SEQUENCE quality_memory_id_seq RESTART WITH 101;

-- A quality agent session is one target's long-lived conversation with the built-in
-- agent: every wake appends messages to the same thread. The message table is that
-- transcript (system/user/assistant/tool/engine/compaction); the runner's wake
-- scheduling and budgets live on the session row. "Running" is expressed by the lease,
-- never by the state.
CREATE TABLE IF NOT EXISTS quality_agent_session (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentSessions/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    target_type INT2 NOT NULL DEFAULT 0,
    -- The database allowlist entry this session is scoped to.
    database_ref TEXT NOT NULL DEFAULT '',
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- 1 ENABLED, 3 BLOCKED, 4 ARCHIVED (QualityAgentSessionState).
    state INT2 NOT NULL DEFAULT 1,
    blocked_code TEXT NOT NULL DEFAULT '',
    blocked_reason TEXT NOT NULL DEFAULT '',
    revision INT4 NOT NULL DEFAULT 1,
    wake_count INT4 NOT NULL DEFAULT 0,
    -- The rolling checkpoint summary and the message seq it replaces in model context.
    summary TEXT NOT NULL DEFAULT '',
    summary_through_seq INT8 NOT NULL DEFAULT 0,
    -- The largest message seq the last wake consumed; a newer seq is a pending event.
    processed_seq INT8 NOT NULL DEFAULT 0,
    -- Stored as QualityAgentSessionEngineState (proto/store/store/data_quality.proto)
    engine_state JSONB NOT NULL DEFAULT '{}',
    next_wake_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    restart_count INT4 NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ,
    -- 1 MONITOR, 2 TRIAGE (QualityAgentSessionMode).
    mode INT2 NOT NULL DEFAULT 1,
    -- 1 TARGET work thread, 2 CONVERSATION front office.
    kind INT2 NOT NULL DEFAULT 1,
    CONSTRAINT quality_agent_session_kind_check
        CHECK ((kind = 1 AND target_guid <> '') OR (kind = 2 AND target_guid = ''))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_resource
    ON quality_agent_session(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_target
    ON quality_agent_session(agent_id, target_guid)
    WHERE archived_at IS NULL AND kind = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_conversation
    ON quality_agent_session(agent_id)
    WHERE archived_at IS NULL AND kind = 2;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_agent_due
    ON quality_agent_session(agent_id, state, next_wake_at)
    WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_due
    ON quality_agent_session(state, next_wake_at) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_workspace
    ON quality_agent_session(workspace_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_database
    ON quality_agent_session(database_ref) WHERE archived_at IS NULL;
COMMENT ON TABLE quality_agent_session IS 'One long-lived agent conversation per target, with wake scheduling and budgets';
COMMENT ON COLUMN quality_agent_session.state IS 'Stored as QualityAgentSessionState (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.mode IS 'Stored as QualityAgentSessionMode (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.engine_state IS 'Stored as QualityAgentSessionEngineState (proto/store/store/data_quality.proto); deterministic wake bookkeeping';
COMMENT ON COLUMN quality_agent_session.summary IS 'Rolling checkpoint summary that replaces messages up to summary_through_seq in model context';
COMMENT ON COLUMN quality_agent_session.kind IS '1 TARGET work thread, 2 CONVERSATION front office';
ALTER SEQUENCE quality_agent_session_id_seq RESTART WITH 101;

-- One row per message in a session's conversation. assistant rows carry their
-- tool_calls JSON so the row maps 1:1 to a provider assistant message; tool rows carry
-- the result the model actually saw. engine rows are runner/UI notes and are never sent
-- back to the model.
CREATE TABLE IF NOT EXISTS quality_agent_message (
    id BIGSERIAL PRIMARY KEY,
    session_id INT8 NOT NULL REFERENCES quality_agent_session(id) ON DELETE CASCADE,
    workspace_id TEXT COLLATE "C" NOT NULL,
    -- Monotonic within one session; the read API pages and polls with after_seq.
    seq INT8 NOT NULL,
    -- The wake and LLM turn the message belongs to, for grouping and debugging.
    wake INT4 NOT NULL DEFAULT 0,
    turn INT4 NOT NULL DEFAULT 0,
    -- 1 SYSTEM, 2 USER, 3 ASSISTANT, 4 TOOL, 5 ENGINE, 6 COMPACTION (QualityAgentMessageRole).
    role INT2 NOT NULL,
    -- 1 WAKE, 2 EVENT, 3 ASK, 4 RUNNER, 5 MODEL, 6 COMPACTION (QualityAgentMessageOrigin).
    origin INT2 NOT NULL DEFAULT 0,
    content TEXT NOT NULL DEFAULT '',
    -- Stored as QualityToolCallList (proto/store/store/data_quality.proto)
    tool_calls JSONB NOT NULL DEFAULT '{}',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    -- The object an event or wake is about (for example a quality execution
    -- resource name); empty for model and runner messages.
    reference TEXT NOT NULL DEFAULT '',
    -- Only the total token count is stored; prompt/completion splits are not.
    tokens INT4 NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    -- The store returns the existing row for a retried key, so a repeated wake or a
    -- retried effect cannot duplicate the conversation.
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_message_seq
    ON quality_agent_message(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_message_key
    ON quality_agent_message(session_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_quality_agent_message_workspace
    ON quality_agent_message(workspace_id, created_at DESC);
COMMENT ON TABLE quality_agent_message IS 'Append-only transcript of one quality agent session';
COMMENT ON COLUMN quality_agent_message.role IS 'Stored as QualityAgentMessageRole (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.origin IS 'Stored as QualityAgentMessageOrigin (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.tool_calls IS 'Stored as QualityToolCallList (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_message_id_seq RESTART WITH 101;

-- One row per monitorable object in the runner's database scope: the desired
-- enrollment state, the admission priority and the session the queue created. The
-- session row stays the execution state; this table is the queue.
CREATE TABLE IF NOT EXISTS quality_agent_enrollment (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentEnrollments/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- instances/{instance}/databases/{database}.
    database_ref TEXT NOT NULL,
    -- 1 PENDING, 2 ACTIVE, 3 DORMANT, 5 OUT_OF_SCOPE, 6 GONE
    -- (QualityAgentEnrollmentState).
    state INT2 NOT NULL DEFAULT 1,
    -- 1 MONITOR, 2 TRIAGE (QualityAgentSessionMode): what admission will create.
    mode INT2 NOT NULL DEFAULT 1,
    -- 0..1000; larger is admitted first.
    priority INT4 NOT NULL DEFAULT 0,
    -- Human-readable signals behind the score, for example "rules,failing".
    priority_reason TEXT NOT NULL DEFAULT '',
    -- Why the row is not progressing: a budget, a person or a missing dependency.
    blocked_reason TEXT NOT NULL DEFAULT '',
    session_id INT8 REFERENCES quality_agent_session(id) ON DELETE SET NULL,
    triage_count INT4 NOT NULL DEFAULT 0,
    last_triaged_at TIMESTAMPTZ,
    -- When a dormant row is due for another look; NULL while pending or active.
    next_review_at TIMESTAMPTZ,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revision INT4 NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_quality_agent_enrollment_target_agent UNIQUE (agent_id, target_guid)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_enrollment_resource
    ON quality_agent_enrollment(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_agent_admit
    ON quality_agent_enrollment(agent_id, state, priority DESC, target_guid);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_agent_database
    ON quality_agent_enrollment(agent_id, database_ref);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_review
    ON quality_agent_enrollment(state, next_review_at);
COMMENT ON TABLE quality_agent_enrollment IS 'One target''s place in the quality agent runner''s queue: desired state, priority and session link';
COMMENT ON COLUMN quality_agent_enrollment.state IS 'Stored as QualityAgentEnrollmentState (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_enrollment.mode IS 'Stored as QualityAgentSessionMode (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_enrollment_id_seq RESTART WITH 101;

-- Per-agent patrol reports. A report is a synchronous deterministic snapshot over the
-- agent's database scope for one scheduled DAILY/WEEKLY period; the body is rendered
-- markdown so the report can be read in the conversation and copied out, while the
-- columns keep the counts queryable without parsing the body.
CREATE TABLE IF NOT EXISTS quality_agent_report (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentReports/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- 1 DAILY, 2 WEEKLY (QualityAgentReportPeriod); every report is scheduled.
    period_kind INT2 NOT NULL DEFAULT 1,
    period_key TEXT NOT NULL DEFAULT '',
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
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
    body_markdown TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    generated_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_report_resource
    ON quality_agent_report(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_report_period
    ON quality_agent_report(agent_id, period_key);
CREATE INDEX IF NOT EXISTS idx_quality_agent_report_agent_created
    ON quality_agent_report(agent_id, created_at DESC);
COMMENT ON TABLE quality_agent_report IS 'Per-agent deterministic patrol report over the agent database scope';
COMMENT ON COLUMN quality_agent_report.scope IS 'Database resource names the report covers';
COMMENT ON COLUMN quality_agent_report.body_markdown IS 'Rendered report body; counts are also stored as columns';

-- A quality agent's escalation: something needs its owner. The notification is the
-- doorbell; this table is the durable to-do item that survives the notification and can
-- be answered from the agent home.
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
    title TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    -- 1 OPEN, 2 RESOLVED, 3 DISMISSED (QualityAgentAttentionStatus).
    status INT2 NOT NULL DEFAULT 1,
    -- Idempotency: one open escalation per agent/kind. A retried runner pass or a
    -- re-blocked session cannot page the owner twice.
    dedupe_key TEXT NOT NULL DEFAULT '',
    resolved_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
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

-- The adoption ledger: one row per suggestion the agent adopted (or tried to). It is
-- the audit trail behind the count the daily guardrail enforces, and it keeps the
-- decision attributable to an agent rather than to a person.
CREATE TABLE IF NOT EXISTS quality_agent_adoption (
    id BIGSERIAL PRIMARY KEY,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    suggestion_id INT8 NOT NULL REFERENCES quality_suggestion(id) ON DELETE CASCADE,
    -- 1 ADOPTED, 2 SKIPPED, 3 FAILED (QualityAgentAdoptionOutcome); 0 is unset.
    outcome INT2 NOT NULL DEFAULT 0 CHECK (outcome IN (0, 1, 2, 3)),
    rule_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_adoption_agent_suggestion
    ON quality_agent_adoption(agent_id, suggestion_id);
CREATE INDEX IF NOT EXISTS idx_quality_agent_adoption_agent_created
    ON quality_agent_adoption(agent_id, created_at DESC);
COMMENT ON TABLE quality_agent_adoption IS 'One row per suggestion an agent auto-adopted or skipped, the daily-cap audit trail';
COMMENT ON COLUMN quality_agent_adoption.outcome IS '1 ADOPTED, 2 SKIPPED, 3 FAILED; 0 is unset and never written by the code';

ALTER TABLE quality_suggestion ADD CONSTRAINT quality_suggestion_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES quality_agent(id) ON DELETE SET NULL;

-- The rebuilt quality_agent rows no longer reference the old agent principals. Soft-delete
-- those internal accounts so they disappear from the user directory; the audit trail can
-- still resolve the actor.
UPDATE principal SET deleted = TRUE
WHERE email LIKE 'agent-%@agents.metaxisdata.local' AND deleted = FALSE
  AND NOT EXISTS (SELECT 1 FROM quality_agent a WHERE a.principal_id = principal.id);
