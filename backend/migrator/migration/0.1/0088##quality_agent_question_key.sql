-- A QUESTION stores the slug its subagent chose, so the wake prompt can show the exact
-- question_key to reuse instead of making the model parse the internal dedupe key.
ALTER TABLE quality_agent_request
    ADD COLUMN IF NOT EXISTS question_key TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN quality_agent_request.question_key IS 'The subagent-chosen question_key slug; empty when the case or wording identifies the question';
-- The fingerprint now includes the request kind: a question must not resolve to a
-- decision or proposal that happens to carry the same text.
COMMENT ON COLUMN quality_agent_request.question_fingerprint IS 'Hash of target, case, kind and normalized question; catches a retry with a drifted dedupe key';
