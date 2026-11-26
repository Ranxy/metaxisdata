-- How far one agent may go on one target. The agent row is the delegation, the database row
-- is what the operator chose when they delegated, and the enrollment row is an explicit
-- per-target exception; NULL inherits from the level above, so a narrower level is an
-- addition rather than a rewrite. 2 (REPAIR) is the delegation default: the agent maintains
-- the target, while changing what a check considers acceptable is an opt-in.
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS participation INT2 NOT NULL DEFAULT 2
    CHECK (participation BETWEEN 1 AND 3);
ALTER TABLE quality_agent_database
    ADD COLUMN IF NOT EXISTS participation INT2
    CHECK (participation IS NULL OR participation BETWEEN 1 AND 3);
ALTER TABLE quality_agent_enrollment
    ADD COLUMN IF NOT EXISTS participation INT2
    CHECK (participation IS NULL OR participation BETWEEN 1 AND 3);
COMMENT ON COLUMN quality_agent.participation IS '1 OBSERVE, 2 REPAIR, 3 TUNE: the delegation default for this agent';
COMMENT ON COLUMN quality_agent_database.participation IS 'Optional override at the moment the database was delegated; NULL inherits the agent';
COMMENT ON COLUMN quality_agent_enrollment.participation IS 'Optional per-target exception; NULL inherits the database';
