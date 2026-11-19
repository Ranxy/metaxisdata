-- Each quality agent pins one provider profile and one model. The columns default to
-- '' because CreateQualityAgent inserts the identity row before the API writes the
-- configuration; the API requires the pair, and an empty pair reads as "not
-- configured". No backfill: quality agents have never shipped.
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS provider_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE quality_agent
    ADD COLUMN IF NOT EXISTS provider_model TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_agent.provider_profile IS 'LLM provider profile resource name; set with provider_model and validated against the enabled models of the allowed profiles';
COMMENT ON COLUMN quality_agent.provider_model IS 'Model the agent runs on; the profile must enable it. Empty means not configured';
