ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_agent.language IS 'BCP-47 language for owner-visible agent prose; empty follows the owner profile';
