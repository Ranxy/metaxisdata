-- One case per problem the agent is working on: a check that cannot produce a verdict
-- turns into durable work whose state survives a restart, a model change and a
-- compaction. A case is not an issue: a FAIL is a conclusion about the data and stays in
-- the issue ledger, so no case ever carries FAIL.
CREATE TABLE IF NOT EXISTS quality_agent_case (
    id BIGSERIAL PRIMARY KEY,
    -- workspaces/{workspace}/qualityAgentCases/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- The check the case is about; NULL for a target-level case.
    rule_id INT8 REFERENCES quality_rule(id) ON DELETE CASCADE,
    -- The subject identity an open case claims: target, rule and kind. It exists as a
    -- column because a partial unique index over COALESCE(rule_id, 0) cannot be named as
    -- an ON CONFLICT target, and the upsert is what makes opening a case idempotent.
    subject_key TEXT COLLATE "C" NOT NULL DEFAULT '',
    -- The target's instance at open time, for the connection budget and for reporting.
    instance TEXT NOT NULL DEFAULT '',
    -- EXECUTION for a check that cannot conclude; COVERAGE and STRUCTURE are reserved.
    kind TEXT NOT NULL DEFAULT 'EXECUTION',
    -- The closed FailureClass; empty until the case is diagnosed.
    failure_class TEXT NOT NULL DEFAULT '',
    -- The config field a CONFIG_VALUE_INVALID diagnosis names, in dotted form.
    field TEXT NOT NULL DEFAULT '',
    -- 1 OPEN, 2 DIAGNOSED, 3 ACTED, 4 AWAITING_VERIFICATION, 5 RESOLVED, 6 FROZEN,
    -- 7 ESCALATED (store.QualityAgentCaseState). 5-7 are terminal and release the
    -- subject, so a later occurrence opens a new case instead of staying invisible.
    state INT2 NOT NULL DEFAULT 1 CHECK (state BETWEEN 1 AND 7),
    attempts INT2 NOT NULL DEFAULT 0,
    max_attempts INT2 NOT NULL DEFAULT 2,
    -- The execution the case was opened on: the evidence a repair starts from.
    execution_id INT8 REFERENCES quality_execution(id) ON DELETE SET NULL,
    summary TEXT NOT NULL DEFAULT '',
    -- When the next look is due. A case waiting for a verification run sets this instead
    -- of sleeping until the session's idle cadence.
    next_action_at TIMESTAMPTZ,
    resolution TEXT NOT NULL DEFAULT '',
    -- The case this one supersedes; a terminal case releases its subject, so the next
    -- occurrence links back instead of reopening history.
    previous_case INT8 REFERENCES quality_agent_case(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_case_resource
    ON quality_agent_case(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_case_open_subject
    ON quality_agent_case(agent_id, subject_key) WHERE state BETWEEN 1 AND 4;
CREATE INDEX IF NOT EXISTS idx_quality_agent_case_agent_state
    ON quality_agent_case(agent_id, state, next_action_at);
CREATE INDEX IF NOT EXISTS idx_quality_agent_case_target
    ON quality_agent_case(target_guid, state);
COMMENT ON TABLE quality_agent_case IS 'One durable agent work item: the state a wake resumes from, separate from the issue ledger which holds data conclusions';
COMMENT ON COLUMN quality_agent_case.state IS '1 OPEN, 2 DIAGNOSED, 3 ACTED, 4 AWAITING_VERIFICATION, 5 RESOLVED, 6 FROZEN, 7 ESCALATED; 5-7 release the subject';
COMMENT ON COLUMN quality_agent_case.subject_key IS 'target|rule|kind: the identity an open case claims; one open case per agent and subject';
COMMENT ON COLUMN quality_agent_case.kind IS 'EXECUTION for a check that cannot conclude; COVERAGE and STRUCTURE are reserved';
