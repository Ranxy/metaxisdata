-- The debug log gains the context needed to correlate one LLM call with the
-- operation that failed: the feature kind, a correlation id carried by the
-- caller, and the classifier code when the call failed. Rows written before this
-- migration keep empty values.
ALTER TABLE llm_debug_log ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_debug_log ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE llm_debug_log ADD COLUMN IF NOT EXISTS error_code TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_llm_debug_log_correlation_id ON llm_debug_log(correlation_id);
