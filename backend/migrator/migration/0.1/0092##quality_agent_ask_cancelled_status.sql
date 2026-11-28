-- The lead can withdraw an obsolete question; CANCELLED distinguishes that from the
-- owner's DISMISSED. The status is an int2, so only the catalog comment changes.
COMMENT ON COLUMN quality_agent_ask.status IS '1 OPEN, 2 ANSWERED, 3 RESOLVED, 4 DISMISSED, 5 CANCELLED';
