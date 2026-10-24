-- Suggestions become identity-keyed proposals instead of an append-only log.
--
-- Every suggestion carries a deterministic signature: a label proposal is
-- identified by its column, a rule draft by its type plus the columns it covers.
-- A partial unique index allows one PENDING row per (target, signature), so a
-- regeneration pass refreshes the live proposal instead of appending a duplicate;
-- the database, not the handler, is the arbiter under concurrent requests.
--
-- Rows written before this change get a unique "legacy:" signature so they never
-- collide with each other or with a real proposal. They stay in the queue exactly
-- as before: this is a pre-release feature, so no attempt is made to re-key them.
ALTER TABLE quality_suggestion ADD COLUMN IF NOT EXISTS signature TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE quality_suggestion ADD COLUMN IF NOT EXISTS refreshed_at TIMESTAMPTZ;
UPDATE quality_suggestion SET signature = 'legacy:' || resource_id WHERE signature = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_pending_signature
    ON quality_suggestion(target_guid, target_type, signature)
    WHERE status = 1;

-- Background drafting jobs become request-idempotent: an identical submission
-- (intent + language + targets/columns + force flag) while the first job is still
-- PENDING or RUNNING returns that job instead of starting a second analysis.
ALTER TABLE quality_suggestion_job ADD COLUMN IF NOT EXISTS request_fingerprint TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE quality_suggestion_job ADD COLUMN IF NOT EXISTS force_regenerate BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE quality_suggestion_job SET request_fingerprint = 'legacy:' || resource_id WHERE request_fingerprint = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_job_active_fingerprint
    ON quality_suggestion_job(request_fingerprint)
    WHERE status IN (1, 2);

COMMENT ON COLUMN quality_suggestion.status IS '1 PENDING, 2 ACCEPTED, 3 REJECTED, 4 SUPERSEDED';
COMMENT ON COLUMN quality_suggestion.signature IS 'Deterministic proposal identity: label:<column> or rule:<type>|<sorted columns>|<discriminator>; one PENDING row per (target, signature)';
COMMENT ON COLUMN quality_suggestion.refreshed_at IS 'When a later generation pass refreshed this pending proposal';
COMMENT ON COLUMN quality_suggestion_job.request_fingerprint IS 'Hash of intent, language, target snapshot and force flag; one active job per fingerprint';
COMMENT ON COLUMN quality_suggestion_job.force_regenerate IS 'Whether the job re-proposes already decided suggestions';
