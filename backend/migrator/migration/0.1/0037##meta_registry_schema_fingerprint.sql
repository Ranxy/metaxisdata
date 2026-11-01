-- Per-object structure fingerprint for automatic schema-change detection.
--
-- The registry already stores every table's and view's column list, so a
-- fingerprint derived from that metadata is available for every engine, not just
-- the StarRocks/Doris pair whose DDL is captured in
-- meta_registry_resource_schema. It is what a data-quality check compares against
-- the snapshot taken when it was saved: "the shape you were written against has
-- moved" now holds on MySQL, PostgreSQL and SQL Server too.
--
-- The row is keyed like the metadata row and written by the schema syncer in the
-- same transaction, so it exists only while its metadata row does. No backfill
-- is needed: the first schema sync after this migration writes the fingerprint of
-- every object it syncs, and an object whose metadata did not change keeps the
-- row it got then.
CREATE TABLE IF NOT EXISTS meta_registry_schema_fingerprint (
    guid TEXT COLLATE "C" NOT NULL,
    object_type INT2 NOT NULL,
    fingerprint BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guid, object_type)
);
