-- A contract can hold a plan draft before it is published: the coverage a person
-- is preparing, the published version it was written against, and who last wrote
-- it. Publishing consumes the draft; there is no second plan table.
ALTER TABLE quality_contract
    ADD COLUMN IF NOT EXISTS staged JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS staged_base_version INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS staged_updated_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS staged_updated_at TIMESTAMPTZ;
COMMENT ON COLUMN quality_contract.staged IS 'Stored as QualityContractPlan (proto/store/store/data_quality.proto); the unpublished plan draft';
COMMENT ON COLUMN quality_contract.staged_base_version IS 'Published version the draft was written against; 0 when nothing is staged';
COMMENT ON COLUMN quality_contract.staged_updated_by IS 'Principal id of the last draft writer';
