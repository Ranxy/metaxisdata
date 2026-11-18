-- Review follow-ups on the multi-agent schema.
-- The adoption default is PAUSED (2), not OFF: a newly created agent that selects
-- adoption starts with paused checks for its owner to enable. 0054's comment said
-- "0 OFF, 1 PAUSED, 2 ACTIVE" but the store/v1 enum is 0 UNSPECIFIED, 1 OFF,
-- 2 PAUSED, 3 ACTIVE.
ALTER TABLE quality_agent_config ALTER COLUMN adoption_mode SET DEFAULT 2;
COMMENT ON COLUMN quality_agent_config.adoption_mode IS '0 UNSPECIFIED, 1 OFF, 2 PAUSED, 3 ACTIVE (QualityAgentAdoptionMode)';

-- A target work thread must name a target; only the front-office conversation may
-- have an empty target_guid. The earlier check allowed NULL to pass.
ALTER TABLE quality_agent_session DROP CONSTRAINT IF EXISTS quality_agent_session_kind_check;
ALTER TABLE quality_agent_session ADD CONSTRAINT quality_agent_session_kind_check
    CHECK ((kind = 1 AND target_guid IS NOT NULL AND target_guid <> '') OR
           (kind = 2 AND target_guid = ''));

-- The resolver is a person; deleting the account should clear the resolution
-- pointer like it does elsewhere.
ALTER TABLE quality_agent_attention DROP CONSTRAINT IF EXISTS quality_agent_attention_resolved_by_fkey;
ALTER TABLE quality_agent_attention ADD CONSTRAINT quality_agent_attention_resolved_by_fkey
    FOREIGN KEY (resolved_by) REFERENCES principal(id) ON DELETE SET NULL;
