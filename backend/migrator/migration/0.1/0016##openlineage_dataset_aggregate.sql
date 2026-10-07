-- The dataset pages answered their list by grouping openlineage_run_dataset on
-- every request, so one page load had to read every reference row the database
-- held before it could return the 5000 most recent datasets (measured at ~4.5s
-- over a million references). The aggregate is now maintained as runs are
-- ingested, and the list, its filter menus and the detail's summary read one
-- small row per dataset instead.

-- One row per dataset the ingested runs referenced. The row's existence means
-- the dataset still has references: the store removes it when the last one goes
-- away. ref_count and column_lineage_ref_count count references, the job counts
-- count distinct task GUIDs per direction, and last_seen is the newest event
-- time of the references.
CREATE TABLE IF NOT EXISTS openlineage_dataset (
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    last_seen TIMESTAMPTZ,
    ref_count BIGINT NOT NULL DEFAULT 0,
    source_job_count INTEGER NOT NULL DEFAULT 0,
    target_job_count INTEGER NOT NULL DEFAULT 0,
    column_lineage_ref_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name)
);

-- The dataset list reads the most recently seen datasets, so the window is an
-- index scan rather than a sort of the whole table.
CREATE INDEX IF NOT EXISTS idx_openlineage_dataset_last_seen
    ON openlineage_dataset(last_seen DESC NULLS LAST);

-- One row per (dataset, kind, value) with the number of references carrying it,
-- which is what makes the aggregate exact under a redelivery: kind is
-- 'task:input' or 'task:output' (value is the task GUID) or 'integration' or
-- 'source' (value is what the runs reported). The job counts are the number of
-- task rows per direction and the arrays the list shows are read from the value
-- rows, so a reference that goes away takes its contribution back out again.
-- The primary key serves both the array reads and the integration/source
-- filters; the longest value it holds is a task GUID, which ingestion already
-- caps below PostgreSQL's index item size.
CREATE TABLE IF NOT EXISTS openlineage_dataset_member (
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    value TEXT NOT NULL,
    ref_count BIGINT NOT NULL,
    PRIMARY KEY (namespace, name, kind, value),
    FOREIGN KEY (namespace, name) REFERENCES openlineage_dataset(namespace, name) ON DELETE CASCADE
);
