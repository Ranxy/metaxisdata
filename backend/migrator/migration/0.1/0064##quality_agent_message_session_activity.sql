CREATE INDEX IF NOT EXISTS idx_quality_agent_message_session_activity
    ON quality_agent_message(session_id, created_at DESC);
