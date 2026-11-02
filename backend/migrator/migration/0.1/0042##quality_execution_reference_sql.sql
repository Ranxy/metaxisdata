-- A cross-table reconciliation whose reference relation lives in another database
-- or instance reads it over its own read-only connection, so the execution records
-- that second statement beside executed_sql. Empty for every other execution and
-- for a reconciliation whose two sides shared one statement.
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS reference_executed_sql TEXT NOT NULL DEFAULT '';
