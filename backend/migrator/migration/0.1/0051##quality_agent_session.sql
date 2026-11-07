-- Replace the goal substrate with one long-lived session per target. Goals could not
-- keep a conversation: each round started a fresh model conversation and the step
-- journal stored only bounded summaries, so a target's agent history was neither
-- continuous nor readable as a transcript. The session tables below are the durable
-- conversation and its wake schedule; the old tables are dropped outright because no
-- deployment predates this change.
DROP TABLE IF EXISTS quality_agent_goal_step;
DROP TABLE IF EXISTS quality_agent_goal;

-- A quality agent session is one target's long-lived conversation with the built-in
-- agent: every wake appends messages to the same thread instead of starting a fresh
-- round. The message table is that transcript (system/user/assistant/tool/engine/
-- compaction); the runner's wake scheduling and budgets live on the session row.
-- This replaces the goal substrate: a goal was one objective with an independent
-- per-round conversation, which could not answer "what has the agent done for this
-- table" or "what input is it working from".
CREATE TABLE IF NOT EXISTS quality_agent_session (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentSessions/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL DEFAULT 0,
    -- The database allowlist entry this session is scoped to.
    database_ref TEXT NOT NULL DEFAULT '',
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    objective TEXT NOT NULL DEFAULT '',
    -- 1 ENABLED, 2 PAUSED, 3 BLOCKED, 4 ARCHIVED (QualityAgentSessionState).
    state INT2 NOT NULL DEFAULT 1,
    blocked_code TEXT NOT NULL DEFAULT '',
    blocked_reason TEXT NOT NULL DEFAULT '',
    revision INT4 NOT NULL DEFAULT 1,
    wake_count INT4 NOT NULL DEFAULT 0,
    -- Lifetime tokens, so a cleared/archived session still counts in metrics.
    tokens_used INT8 NOT NULL DEFAULT 0,
    -- The largest real prompt_tokens of the last wake; the compaction trigger.
    context_tokens INT4 NOT NULL DEFAULT 0,
    -- The rolling checkpoint summary and the message seq it replaces in model context.
    summary TEXT NOT NULL DEFAULT '',
    summary_through_seq INT8 NOT NULL DEFAULT 0,
    -- The largest message seq the last wake consumed; a newer seq is a pending event.
    processed_seq INT8 NOT NULL DEFAULT 0,
    -- Deterministic-engine bookkeeping (done gaps, blocked rounds).
    engine_state JSONB NOT NULL DEFAULT '{}',
    next_wake_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    restart_count INT4 NOT NULL DEFAULT 0,
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_resource
    ON quality_agent_session(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_target
    ON quality_agent_session(workspace_id, target_guid) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_due
    ON quality_agent_session(state, next_wake_at) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_workspace
    ON quality_agent_session(workspace_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_database
    ON quality_agent_session(database_ref) WHERE archived_at IS NULL;
COMMENT ON TABLE quality_agent_session IS 'One long-lived agent conversation per target, with wake scheduling and budgets';
COMMENT ON COLUMN quality_agent_session.state IS 'Stored as QualityAgentSessionState (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.engine_state IS 'Deterministic wake bookkeeping; not a user-facing state';
COMMENT ON COLUMN quality_agent_session.summary IS 'Rolling checkpoint summary that replaces messages up to summary_through_seq in model context';
ALTER SEQUENCE quality_agent_session_id_seq RESTART WITH 101;

-- One row per message in a session's conversation. assistant rows carry their
-- tool_calls JSON so the row maps 1:1 to a provider assistant message; tool rows
-- carry the result the model actually saw. engine rows are runner/UI notes and are
-- never sent back to the model.
CREATE TABLE IF NOT EXISTS quality_agent_session_message (
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
    -- QualityToolCallList JSONB: the assistant message's tool calls.
    tool_calls JSONB NOT NULL DEFAULT '[]',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    tokens INT4 NOT NULL DEFAULT 0,
    prompt_tokens INT4 NOT NULL DEFAULT 0,
    completion_tokens INT4 NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    -- The store returns the existing row for a retried key, so a repeated wake or a
    -- retried effect cannot duplicate the conversation.
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_message_seq
    ON quality_agent_session_message(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_message_key
    ON quality_agent_session_message(session_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_message_workspace
    ON quality_agent_session_message(workspace_id, created_at DESC);
COMMENT ON TABLE quality_agent_session_message IS 'Append-only transcript of one quality agent session';
COMMENT ON COLUMN quality_agent_session_message.role IS 'Stored as QualityAgentMessageRole (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session_message.origin IS 'Stored as QualityAgentMessageOrigin (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session_message.tool_calls IS 'Stored as QualityToolCallList (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_session_message_id_seq RESTART WITH 101;
