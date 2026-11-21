-- The last model-input sample the session engine measured. The session detail
-- renders it as the context meter; it is an observability snapshot, not the token
-- spend, which stays on the transcript rows.
ALTER TABLE quality_agent_session
    ADD COLUMN IF NOT EXISTS context_usage JSONB NOT NULL DEFAULT '{}';
COMMENT ON COLUMN quality_agent_session.context_usage IS 'Stored as QualityAgentSessionContextUsage (proto/store/store/data_quality.proto); last measured model-input size and the pinned model capacities at that moment';
