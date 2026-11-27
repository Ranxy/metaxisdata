-- One active ask per (agent, dedupe key). Approval and anomaly asks can be raised by
-- more than one pass, and a repeated question must reuse the open row instead of
-- ringing the owner again.
--
-- Replayable on LATEST: the column and index already exist there.

ALTER TABLE quality_agent_ask
    ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_ask_dedupe
    ON quality_agent_ask(agent_id, dedupe_key)
    WHERE dedupe_key <> '' AND status IN (1, 2);
COMMENT ON COLUMN quality_agent_ask.dedupe_key IS 'One active ask per agent and key; empty means not deduped';
