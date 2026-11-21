-- The adoption ledger now records who started the adoption, because the daily
-- autonomous ceiling must not charge a person's approval. Existing rows cannot
-- tell the two apart; 1 (AUTO) keeps their current budget for at most the 24h
-- window instead of handing out extra adoptions on deploy.
ALTER TABLE quality_agent_adoption
    ADD COLUMN IF NOT EXISTS origin INT2 NOT NULL DEFAULT 1;
ALTER TABLE quality_agent_adoption DROP CONSTRAINT IF EXISTS quality_agent_adoption_origin_check;
ALTER TABLE quality_agent_adoption
    ADD CONSTRAINT quality_agent_adoption_origin_check CHECK (origin IN (1, 2, 3));
COMMENT ON TABLE quality_agent_adoption IS 'One row per suggestion an agent adopted or skipped, or a person approved: the daily-cap audit trail and the approval record';
COMMENT ON COLUMN quality_agent_adoption.origin IS '1 AUTO wake pass, 2 TOOL owner-requested tool, 3 APPROVAL a person''s decision';
