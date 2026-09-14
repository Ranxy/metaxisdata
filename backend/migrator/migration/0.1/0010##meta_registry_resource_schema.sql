-- Per-object DDL storage for meta registry resources.
--
-- meta_registry_resource_schema holds the engine's own DDL (SHOW CREATE ...) for
-- a metadata object, keyed by the same (guid, object_type) as
-- meta_registry_resource. It exists because reconstructing DDL from metadata is
-- lossy for StarRocks/Doris (no DISTRIBUTED BY, properties, partitioning,
-- rollups), and those engines have no reconstruction path at all.
--
-- A row is created/updated/deleted in the same transaction as its metadata row.
-- A missing row is the normal state for engines whose DDL is not fetched, and
-- the read path falls back to metadata reconstruction.
--
-- No backfill is needed: the first schema sync after this migration populates
-- every row.

CREATE TABLE IF NOT EXISTS meta_registry_resource_schema (
    guid TEXT COLLATE "C" NOT NULL,
    object_type INT2 NOT NULL,
    schema TEXT NOT NULL,
    schema_hash BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guid, object_type)
);
