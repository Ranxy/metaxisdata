-- A pin is the only way to take a check out of an agent's reach. It is explicit and
-- revocable, and editing a check never creates one: the alternative (authorship) made one
-- human edit disable the agent forever, which is the opposite of delegating a database.
--
-- manual_lock pins the whole check; pinned_fields pins only the listed config fields, so
-- "this number is mine, the rest is yours" is expressible. Both are head-only metadata like
-- frequency_tier: they are not part of the definition and write no revision.
ALTER TABLE quality_rule ADD COLUMN IF NOT EXISTS manual_lock BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE quality_rule ADD COLUMN IF NOT EXISTS pinned_fields TEXT[] NOT NULL DEFAULT '{}';
COMMENT ON COLUMN quality_rule.manual_lock IS 'A person manages this check; the agent may only propose changes to it';
COMMENT ON COLUMN quality_rule.pinned_fields IS 'Config fields a person pinned, in dotted form (range.min); the agent may not rewrite them';
