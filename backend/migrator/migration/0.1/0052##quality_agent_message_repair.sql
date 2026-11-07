-- Repair for pre-release databases that applied the first version of 0051 before the
-- transcript table was renamed (quality_agent_session_message -> quality_agent_message,
-- tool_calls default '[]' -> '{}', reference column). Fresh installations already have
-- the final schema, so every statement below is a no-op for them.
DO $$
BEGIN
    IF to_regclass('quality_agent_message') IS NULL
        AND to_regclass('quality_agent_session_message') IS NOT NULL THEN
        ALTER TABLE quality_agent_session_message RENAME TO quality_agent_message;
    END IF;
END $$;
ALTER TABLE quality_agent_message ALTER COLUMN tool_calls SET DEFAULT '{}';
ALTER TABLE quality_agent_message ADD COLUMN IF NOT EXISTS reference TEXT NOT NULL DEFAULT '';
ALTER SEQUENCE IF EXISTS quality_agent_session_message_id_seq RENAME TO quality_agent_message_id_seq;
ALTER INDEX IF EXISTS idx_quality_agent_session_message_seq RENAME TO idx_quality_agent_message_seq;
ALTER INDEX IF EXISTS idx_quality_agent_session_message_key RENAME TO idx_quality_agent_message_key;
ALTER INDEX IF EXISTS idx_quality_agent_session_message_workspace RENAME TO idx_quality_agent_message_workspace;
