-- A repeated question must resolve to the same active request even when the model
-- changes its dedupe key between retries. The fingerprint is target + case + the
-- whitespace-normalized question text, and the partial index only covers active rows.
ALTER TABLE quality_agent_request
    ADD COLUMN IF NOT EXISTS question_fingerprint TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_request_fingerprint
    ON quality_agent_request(agent_id, question_fingerprint)
    WHERE question_fingerprint <> '' AND status IN (1, 2);
COMMENT ON COLUMN quality_agent_request.question_fingerprint IS 'Hash of target, case and normalized question; catches a retry with a drifted dedupe key';
