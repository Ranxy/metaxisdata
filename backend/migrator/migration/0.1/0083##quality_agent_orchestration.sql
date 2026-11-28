-- Pre-release rebuild for the A architecture of the quality agent.
--
-- The quality agent has never shipped. This migration rebuilds the runtime tables:
-- every target now has a fixed SUBAGENT; the LEAD is the owner conversation; requests
-- and asks replace the per-target attention hand-off; the session carries
-- last_reviewed_at for the deterministic daily review. Existing development rows are
-- discarded (data loss accepted, no backfill), following the 0062 precedent.
--
-- The same final shape is what migration/LATEST.sql creates for a fresh install, so
-- this file is replayable on top of LATEST: every drop is IF EXISTS and every create is
-- IF NOT EXISTS.

-- Children first: requests, asks, attention and enrollment reference the session.
DROP TABLE IF EXISTS quality_agent_request;
DROP TABLE IF EXISTS quality_agent_ask;
DROP TABLE IF EXISTS quality_agent_attention;
DROP TABLE IF EXISTS quality_agent_enrollment;
DROP TABLE IF EXISTS quality_agent_message;
DROP TABLE IF EXISTS quality_agent_session;

-- A quality agent session is one durable agent conversation. LEAD is the
-- owner-facing coordinator; SUBAGENT owns exactly one target. Every target gets
-- one SUBAGENT when it is discovered, and the daily review is deterministic until
-- actionable evidence calls the model. "Running" is expressed by the lease, never
-- by the state.
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
    -- 1 LEAD, 2 SUBAGENT (QualityAgentSessionRole).
    role INT2 NOT NULL DEFAULT 2,
    -- The lead session a SUBAGENT belongs to; NULL for LEAD.
    parent_session_id INT8 REFERENCES quality_agent_session(id) ON DELETE CASCADE,
    -- Model-facing unique name of a SUBAGENT; empty for LEAD.
    subagent_name TEXT NOT NULL DEFAULT '',
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
    -- The last deterministic daily review that found nothing actionable.
    last_reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ,
    -- 1 MONITOR, 2 TRIAGE (QualityAgentSessionMode).
    mode INT2 NOT NULL DEFAULT 1,
    -- The last model-input sample the session engine measured; observability only.
    context_usage JSONB NOT NULL DEFAULT '{}',
    CONSTRAINT quality_agent_session_role_check
        CHECK ((role = 1 AND target_guid = '' AND parent_session_id IS NULL)
            OR (role = 2 AND target_guid <> '' AND parent_session_id IS NOT NULL AND subagent_name <> ''))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_resource
    ON quality_agent_session(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_target
    ON quality_agent_session(agent_id, target_guid)
    WHERE archived_at IS NULL AND role = 2;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_lead
    ON quality_agent_session(agent_id)
    WHERE archived_at IS NULL AND role = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_subagent_name
    ON quality_agent_session(agent_id, subagent_name)
    WHERE archived_at IS NULL AND role = 2 AND subagent_name <> '';
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_agent_due
    ON quality_agent_session(agent_id, state, next_wake_at)
    WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_due
    ON quality_agent_session(state, next_wake_at) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_workspace
    ON quality_agent_session(workspace_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_database
    ON quality_agent_session(database_ref) WHERE archived_at IS NULL;
COMMENT ON TABLE quality_agent_session IS 'One durable agent conversation per lead or target; the A architecture materializes a SUBAGENT for every discovered target';
COMMENT ON COLUMN quality_agent_session.role IS '1 LEAD coordinator, 2 SUBAGENT one target (QualityAgentSessionRole)';
COMMENT ON COLUMN quality_agent_session.parent_session_id IS 'The lead session a SUBAGENT reports to';
COMMENT ON COLUMN quality_agent_session.subagent_name IS 'Model-facing unique name of a SUBAGENT; empty for LEAD';
COMMENT ON COLUMN quality_agent_session.state IS 'Stored as QualityAgentSessionState (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.mode IS 'Stored as QualityAgentSessionMode (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.engine_state IS 'Stored as QualityAgentSessionEngineState (proto/store/store/data_quality.proto); deterministic wake bookkeeping';
COMMENT ON COLUMN quality_agent_session.summary IS 'Rolling checkpoint summary that replaces messages up to summary_through_seq in model context';
COMMENT ON COLUMN quality_agent_session.last_reviewed_at IS 'Last deterministic daily review that found nothing actionable';
COMMENT ON COLUMN quality_agent_session.context_usage IS 'Stored as QualityAgentSessionContextUsage (proto/store/store/data_quality.proto); last measured model-input size and the pinned model capacities at that moment';
ALTER SEQUENCE quality_agent_session_id_seq RESTART WITH 101;

-- One row per message in a session's conversation. assistant rows carry their
-- tool_calls JSON so the row maps 1:1 to a provider assistant message; tool rows carry
-- the result the model actually saw. engine rows are runner/UI notes and are never sent
-- back to the model. sender_* and audience distinguish owner-facing prose from
-- agent-to-agent traffic.
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
    -- 1 WAKE, 2 EVENT, 3 ASK, 4 RUNNER, 5 MODEL, 6 COMPACTION, 7 AGENT, 8 NOTICE
    -- (QualityAgentMessageOrigin).
    origin INT2 NOT NULL DEFAULT 0,
    content TEXT NOT NULL DEFAULT '',
    -- Display-only model reasoning; never sent back to the provider.
    reasoning TEXT NOT NULL DEFAULT '',
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
    sender_session_id INT8 REFERENCES quality_agent_session(id) ON DELETE SET NULL,
    sender_name TEXT NOT NULL DEFAULT '',
    -- 1 MESSAGE, 2 REPORT, 3 QUESTION, 4 ANSWER, 5 ASSIGN, 6 NOTICE (QualityAgentMessageIntent).
    intent INT2 NOT NULL DEFAULT 1,
    -- 1 OWNER, 2 INTERNAL (QualityAgentMessageAudience).
    audience INT2 NOT NULL DEFAULT 2,
    reply_to TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_message_seq
    ON quality_agent_message(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_message_key
    ON quality_agent_message(session_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_quality_agent_message_session_activity
    ON quality_agent_message(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_message_workspace
    ON quality_agent_message(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_message_audience
    ON quality_agent_message(session_id, audience, seq);
COMMENT ON TABLE quality_agent_message IS 'Transcript of one quality agent session; append-only except the SYSTEM prompt row, rewritten when its rendered content changes';
COMMENT ON COLUMN quality_agent_message.role IS 'Stored as QualityAgentMessageRole (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.origin IS 'Stored as QualityAgentMessageOrigin (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.reasoning IS 'Display-only model reasoning; never sent back to the provider';
COMMENT ON COLUMN quality_agent_message.tool_calls IS 'Stored as QualityToolCallList (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.sender_session_id IS 'Agent session that sent this message; NULL for owner and runner rows';
COMMENT ON COLUMN quality_agent_message.sender_name IS 'Model-facing sender name: lead or a subagent name';
COMMENT ON COLUMN quality_agent_message.intent IS 'Stored as QualityAgentMessageIntent (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_message.audience IS '1 OWNER front office, 2 INTERNAL agent traffic (QualityAgentMessageAudience)';
COMMENT ON COLUMN quality_agent_message.reply_to IS 'Ask or request resource id this message answers';
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

-- One owner-facing question the lead asked. It may cover several requests.
CREATE TABLE IF NOT EXISTS quality_agent_ask (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    conversation_session_id INT8 REFERENCES quality_agent_session(id) ON DELETE CASCADE,
    -- 1 QUESTION, 2 ANOMALY, 3 APPROVAL.
    kind INT2 NOT NULL DEFAULT 1,
    -- The seq of the owner-visible assistant message that states the question.
    message_seq INT8 NOT NULL DEFAULT 0,
    title TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    -- 1 OPEN, 2 ANSWERED, 3 RESOLVED, 4 DISMISSED.
    status INT2 NOT NULL DEFAULT 1,
    answered_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    answered_at TIMESTAMPTZ,
    answer_message_seq INT8 NOT NULL DEFAULT 0,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_ask_resource
    ON quality_agent_ask(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_agent_ask_agent_status
    ON quality_agent_ask(agent_id, status, created_at DESC);
COMMENT ON TABLE quality_agent_ask IS 'One owner-facing question from the lead; it can cover several subagent requests';
COMMENT ON COLUMN quality_agent_ask.status IS '1 OPEN, 2 ANSWERED, 3 RESOLVED, 4 DISMISSED';

-- A subagent request for a decision from the lead. It is the durable hand-off
-- between one target's subagent and the lead; an ask carries one or more requests
-- to the owner when the lead cannot decide.
CREATE TABLE IF NOT EXISTS quality_agent_request (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- The subagent that raised the request.
    origin_session_id INT8 REFERENCES quality_agent_session(id) ON DELETE SET NULL,
    target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    target_type INT2 NOT NULL DEFAULT 0,
    -- 1 DECISION, 2 PROPOSAL, 3 BLOCKED, 4 FROZEN, 5 ANOMALY, 6 QUESTION.
    kind INT2 NOT NULL DEFAULT 1,
    -- 1 OPEN, 2 FORWARDED, 3 ANSWERED, 4 RESOLVED, 5 DISMISSED.
    status INT2 NOT NULL DEFAULT 1,
    title TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    case_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    -- Stored as QualityAgentFieldPatch (proto/store/store/data_quality.proto)
    suggested_patch JSONB,
    dedupe_key TEXT NOT NULL DEFAULT '',
    ask_id INT8 REFERENCES quality_agent_ask(id) ON DELETE SET NULL,
    resolution TEXT NOT NULL DEFAULT '',
    resolved_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    forwarded_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_request_resource
    ON quality_agent_request(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_request_active_dedupe
    ON quality_agent_request(agent_id, dedupe_key) WHERE status IN (1, 2);
CREATE INDEX IF NOT EXISTS idx_quality_agent_request_agent_status
    ON quality_agent_request(agent_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_request_target
    ON quality_agent_request(agent_id, target_guid, status);
COMMENT ON TABLE quality_agent_request IS 'One subagent decision request to the lead; OPEN/FORWARDED are active, ANSWERED/RESOLVED/DISMISSED are closed';
COMMENT ON COLUMN quality_agent_request.status IS '1 OPEN, 2 FORWARDED, 3 ANSWERED, 4 RESOLVED, 5 DISMISSED';
COMMENT ON COLUMN quality_agent_request.suggested_patch IS 'protojson of store.QualityAgentFieldPatch; NULL when the request carries no single change';

-- A: TRIAGE and MONITOR are separate workloads. The old max_active_sessions /
-- max_new_sessions_per_hour columns remain for one release (the runner stops using
-- them in a later phase); these two are the independent TRIAGE budget.
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS max_triage_sessions INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_new_triage_sessions_per_hour INT4 NOT NULL DEFAULT 0;
COMMENT ON COLUMN quality_agent.max_triage_sessions IS 'Concurrent TRIAGE targets; 0 means the server default';
COMMENT ON COLUMN quality_agent.max_new_triage_sessions_per_hour IS 'TRIAGE sessions created per hour; 0 means the server default';


-- Columns added by later migrations that this rebuild must preserve; the increment
-- chain ran those ALTERs before 0083, and the recreated tables would otherwise lose
-- them (LATEST still carries the same ALTERs for fresh installs).
ALTER TABLE quality_agent_session ADD COLUMN IF NOT EXISTS manual_resume_at TIMESTAMPTZ;
ALTER TABLE quality_agent_enrollment ADD COLUMN IF NOT EXISTS participation INT2
    CHECK (participation IS NULL OR participation BETWEEN 1 AND 3);
ALTER TABLE quality_agent_attention ADD COLUMN IF NOT EXISTS case_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE quality_agent_attention ADD COLUMN IF NOT EXISTS suggested_patch JSONB;
COMMENT ON COLUMN quality_agent_attention.case_resource_id IS 'The agent work item the escalation is about, by resource id; empty for an agent-level event';
COMMENT ON COLUMN quality_agent_attention.suggested_patch IS 'protojson of store.QualityAgentFieldPatch; NULL when the escalation carries no single change';
