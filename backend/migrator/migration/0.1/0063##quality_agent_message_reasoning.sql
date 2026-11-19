ALTER TABLE quality_agent_message
    ADD COLUMN IF NOT EXISTS reasoning TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_agent_message.reasoning IS 'Display-only model reasoning; never sent back to the provider';
