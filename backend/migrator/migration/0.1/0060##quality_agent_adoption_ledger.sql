-- The adoption ledger is keyed by agent and suggestion, not suggestion alone: the
-- same suggestion may first be SKIPPED and later ADOPTED when the guardrails
-- change, and the later row must update the earlier one instead of being silently
-- ignored. Outcome 0 means "not decided" and is never written by the code.
DROP INDEX IF EXISTS idx_quality_agent_adoption_suggestion;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_adoption_agent_suggestion
    ON quality_agent_adoption(agent_id, suggestion_id);
ALTER TABLE quality_agent_adoption ALTER COLUMN outcome SET DEFAULT 0;
ALTER TABLE quality_agent_adoption DROP CONSTRAINT IF EXISTS chk_quality_agent_adoption_outcome;
ALTER TABLE quality_agent_adoption ADD CONSTRAINT chk_quality_agent_adoption_outcome
    CHECK (outcome IN (0, 1, 2, 3));
COMMENT ON COLUMN quality_agent_adoption.outcome IS '1 ADOPTED, 2 SKIPPED, 3 FAILED; 0 is unset and never written by the code';
