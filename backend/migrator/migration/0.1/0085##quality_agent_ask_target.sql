-- An approval ask is about one target. The question names it in prose, but the owner
-- card and the approve action need the identity, and the read API must not guess it
-- from the dedupe key.
--
-- Replayable on LATEST: the columns and index already exist there.

ALTER TABLE quality_agent_ask
    ADD COLUMN IF NOT EXISTS target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_type INT2 NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_quality_agent_ask_target
    ON quality_agent_ask(agent_id, target_guid);
COMMENT ON COLUMN quality_agent_ask.target_guid IS 'The target an approval ask is about; empty for an agent-level ask';
COMMENT ON COLUMN quality_agent_ask.target_type IS 'Stored as MetaType (proto/store/store/database.proto)';
