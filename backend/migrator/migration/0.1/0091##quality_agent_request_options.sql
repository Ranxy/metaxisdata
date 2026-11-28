-- A subagent's QUESTION can offer the concrete choices it needs decided. The lead
-- may forward them unchanged to the owner's ask, so the owner clicks the options the
-- target actually proposed instead of reading them out of prose.
ALTER TABLE quality_agent_request
    ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT '{}';
COMMENT ON COLUMN quality_agent_request.options IS 'Stored as QualityAgentRequestChoice (proto/store/store/data_quality.proto); the choices a subagent offered with a QUESTION';
