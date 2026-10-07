-- The dataset pages used to derive their dataset list, detail and filter menus
-- by JSON-parsing the raw payload of every recent run: one request read up to
-- 5000 payloads of up to 8MiB each. Ingesting a few thousand maximum-size
-- events was therefore enough to make a single read request allocate tens of
-- gigabytes. The datasets a run read or wrote are now extracted once, at
-- ingestion, and the pages aggregate them in SQL.

-- Airflow links are derived from the same payload; materializing the (already
-- whitelisted) URL lets the run and task lists answer without the payload.
ALTER TABLE openlineage_run ADD COLUMN IF NOT EXISTS airflow_run_log_url TEXT NOT NULL DEFAULT '';

-- One row per dataset a run read or wrote. The foreign key makes the retention
-- prune and a redelivery's row replacement take the references with them.
-- schema_fields and column_lineage_fields hold facet JSON, like raw_payload on
-- openlineage_run, not a proto/store message.
CREATE TABLE IF NOT EXISTS openlineage_run_dataset (
    id BIGSERIAL PRIMARY KEY,
    run_pk BIGINT NOT NULL REFERENCES openlineage_run(id) ON DELETE CASCADE,
    task_guid TEXT COLLATE "C" NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    -- 'input' for a dataset the run read, 'output' for one it wrote.
    direction TEXT NOT NULL,
    has_column_lineage BOOLEAN NOT NULL DEFAULT FALSE,
    schema_fields JSONB,
    column_lineage_fields JSONB,
    event_time TIMESTAMPTZ,
    integration TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per (run, dataset, direction): the write path replaces a run's
-- references as a set, and this is the key that makes the replacement
-- idempotent under a redelivery.
CREATE UNIQUE INDEX IF NOT EXISTS idx_openlineage_run_dataset_unique ON openlineage_run_dataset(run_pk, namespace, name, direction);

-- The dataset pages aggregate and probe by (namespace, name).
CREATE INDEX IF NOT EXISTS idx_openlineage_run_dataset_group ON openlineage_run_dataset(namespace, name);

-- The dataset detail's related-jobs query groups by task.
CREATE INDEX IF NOT EXISTS idx_openlineage_run_dataset_task ON openlineage_run_dataset(task_guid);

-- Retention deletes runs in event-time order; the list orders datasets by the
-- event time of the runs that reference them.
CREATE INDEX IF NOT EXISTS idx_openlineage_run_dataset_event_time ON openlineage_run_dataset(event_time DESC NULLS LAST);
