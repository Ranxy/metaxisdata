-- A model-drafted suggestion's confidence is now part of the review record: the
-- adoption guardrail compares it with the agent's configured threshold, and the
-- reviewer sees it in the queue. Existing rows keep 0, which is below every
-- threshold and therefore never auto-adopted.
ALTER TABLE quality_suggestion ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0;
COMMENT ON COLUMN quality_suggestion.confidence IS 'Confidence the drafting model reported (0..1); 0 for older rows';

-- The adoption ledger: one row per suggestion the agent adopted (or tried to). It
-- is the audit trail behind the count the daily guardrail enforces, and it keeps
-- the decision attributable to an agent rather than to a person.
CREATE TABLE IF NOT EXISTS quality_agent_adoption (
    id BIGSERIAL PRIMARY KEY,
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    suggestion_id INT8 NOT NULL REFERENCES quality_suggestion(id) ON DELETE CASCADE,
    -- 1 ADOPTED, 2 SKIPPED, 3 FAILED (QualityAgentAdoptionOutcome).
    outcome INT2 NOT NULL DEFAULT 1,
    rule_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_adoption_suggestion
    ON quality_agent_adoption(suggestion_id);
CREATE INDEX IF NOT EXISTS idx_quality_agent_adoption_agent_created
    ON quality_agent_adoption(agent_id, created_at DESC);
COMMENT ON TABLE quality_agent_adoption IS 'One row per suggestion an agent auto-adopted or skipped, the daily-cap audit trail';
