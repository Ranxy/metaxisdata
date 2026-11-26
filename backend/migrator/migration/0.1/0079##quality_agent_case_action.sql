-- One row per write an agent made to a check. The ledger answers two questions the case table
-- cannot: how many writes this agent has spent (the budget), and what it did to which check months
-- later (the audit supplement). It is append-only: a row records a write that happened, never a
-- write that was refused.
CREATE TABLE IF NOT EXISTS quality_agent_case_action (
    id BIGSERIAL PRIMARY KEY,
    -- The work item the write belonged to. A write without one (an owner-requested run) keeps NULL.
    case_id INT8 REFERENCES quality_agent_case(id) ON DELETE CASCADE,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    -- The check's target and instance are copied rather than joined: the budget reads by rule and by
    -- instance, and the ledger stays readable after a rule is archived.
    target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    rule_id INT8 REFERENCES quality_rule(id) ON DELETE SET NULL,
    instance TEXT NOT NULL DEFAULT '',
    -- 1 REVISE, 2 CONTROL, 3 RUN, 4 PROPOSE (store.QualityAgentCaseActionKind).
    action INT2 NOT NULL,
    -- The config field a REVISE/PROPOSE named, in dotted form.
    field TEXT NOT NULL DEFAULT '',
    -- The definition revision the write produced; both are equal for an action that wrote none.
    from_revision INT4 NOT NULL DEFAULT 0,
    to_revision INT4 NOT NULL DEFAULT 0,
    execution_id INT8 REFERENCES quality_execution(id) ON DELETE SET NULL,
    -- 1 APPLIED (store.QualityAgentCaseActionOutcome). Only applied writes are recorded.
    outcome INT2 NOT NULL DEFAULT 1,
    reason TEXT NOT NULL DEFAULT '',
    -- The principal that asked for the write when it was not the agent's own wake (an owner's
    -- request); NULL when the agent acted on its own.
    requested_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_quality_agent_case_action_agent_time
    ON quality_agent_case_action(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_case_action_budget
    ON quality_agent_case_action(agent_id, action, rule_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_agent_case_action_case
    ON quality_agent_case_action(case_id, created_at);
COMMENT ON TABLE quality_agent_case_action IS 'Append-only ledger of the writes an agent made to checks: the budget arithmetic and the audit supplement';
COMMENT ON COLUMN quality_agent_case_action.action IS '1 REVISE, 2 CONTROL, 3 RUN, 4 PROPOSE; only writes that happened are recorded';
COMMENT ON COLUMN quality_agent_case_action.outcome IS '1 APPLIED; a refused write is a metric, not a ledger row';

-- The two write budgets an operator can raise or lower. They sit beside the other per-agent budgets
-- and keep the same convention: zero means the server default.
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS repair_max_per_day INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS run_max_per_rule_per_day INT4 NOT NULL DEFAULT 0;
COMMENT ON COLUMN quality_agent.repair_max_per_day IS 'Writes to checks the agent may make per rolling day; zero means the server default';
COMMENT ON COLUMN quality_agent.run_max_per_rule_per_day IS 'Immediate runs the agent may request per check per rolling day; zero means the server default';
