-- A contract holds its current plan declaration: the coverage a person wrote, the
-- published version it is aligned to, and who last wrote it. Publishing records the
-- declaration and advances that base; there is no second plan table.
ALTER TABLE quality_contract
    ADD COLUMN IF NOT EXISTS staged JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS staged_base_version INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS staged_updated_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS staged_updated_at TIMESTAMPTZ;
COMMENT ON COLUMN quality_contract.staged IS 'Stored as QualityContractPlan (proto/store/store/data_quality.proto); the current plan declaration';
COMMENT ON COLUMN quality_contract.staged_base_version IS 'Newest published version the declaration is aligned to; equals published_version when nothing is staged';
COMMENT ON COLUMN quality_contract.staged_updated_by IS 'Principal id of the last declaration writer';
