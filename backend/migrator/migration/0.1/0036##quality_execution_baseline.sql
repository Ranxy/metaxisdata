-- A row_count_change execution can now carry the baseline it judged against: the
-- row count and the moment that count was measured. The verdict becomes
-- self-describing instead of asking a reader to guess which older execution
-- supplied the denominator. Both stay NULL for every other rule type and for a
-- first observation that had no recent baseline to compare with.
ALTER TABLE quality_execution
    ADD COLUMN IF NOT EXISTS baseline_value DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS baseline_time TIMESTAMPTZ;
