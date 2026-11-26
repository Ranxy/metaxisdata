-- A person resuming a blocked session is a decision the budget must not silently overrule: the
-- runner used to grant the resume and then re-block the session seconds later, so the button looked
-- broken. The marker records that a person asked for a wake, and the next budget check spends it
-- instead of refusing the wake. It is cleared as soon as it is used, so it buys exactly one wake.
ALTER TABLE quality_agent_session ADD COLUMN IF NOT EXISTS manual_resume_at TIMESTAMPTZ;
COMMENT ON COLUMN quality_agent_session.manual_resume_at IS 'Set when a person resumed the session; the next wake bypasses the hourly budget once and clears it';
