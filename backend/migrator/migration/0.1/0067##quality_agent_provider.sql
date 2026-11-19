ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS provider_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS provider_model TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_agent.provider_profile IS 'LLM provider profile resource name; set with provider_model and validated against the enabled profiles';
COMMENT ON COLUMN quality_agent.provider_model IS 'Model the agent runs on; the profile must enable it. Empty means not configured';
