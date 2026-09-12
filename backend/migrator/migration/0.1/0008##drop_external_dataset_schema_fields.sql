-- Drop external_dataset.schema_fields.
--
-- The column never had a writer: OpenLineage ingestion keeps the dataset schema
-- on the run payload, and the OpenLineage dataset views read it from there, so
-- this column was always its default '[]'. The API never exposed it either.

ALTER TABLE external_dataset DROP COLUMN IF EXISTS schema_fields;
