-- column_lineage_version records which analyzer rules produced the stored
-- lineage. The runner compares it with the current build's AnalyzerVersion, so a
-- deployment that changes the analyzers re-analyzes what it already stored
-- instead of leaving stale results in place. '' means "analyzed before the
-- version was recorded" and therefore also re-analyzes once.
ALTER TABLE column_lineage_version
    ADD COLUMN IF NOT EXISTS analyzer_version TEXT NOT NULL DEFAULT '';
