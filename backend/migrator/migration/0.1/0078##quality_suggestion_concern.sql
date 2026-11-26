-- A model that drafts a check can also distrust it: "this will report a structural false positive on
-- an audit table", "the column is usually NULL". Until now that doubt had nowhere to go, and the
-- adoption gate could not read it — the draft was adopted on type, confidence and criticality alone.
-- The concern is head-only metadata beside the payload, never part of the definition: a refresh pass
-- rewrites the payload and must not erase the doubt that was raised about it.
ALTER TABLE quality_suggestion ADD COLUMN IF NOT EXISTS concern TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_suggestion.concern IS 'The agent''s own objection to its draft; a draft carrying one is never adopted automatically';
