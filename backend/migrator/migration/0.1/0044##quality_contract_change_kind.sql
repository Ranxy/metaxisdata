-- A published contract version now records which sections changed against its
-- predecessor (intent / plan / checks / more than one). The kind is computed
-- under the contract row lock at publish time; 0 is the value of versions
-- published before the column existed.
ALTER TABLE quality_contract_revision
    ADD COLUMN IF NOT EXISTS change_kind INT2 NOT NULL DEFAULT 0;
COMMENT ON COLUMN quality_contract_revision.change_kind IS 'Stored as QualityContractChangeKind (proto/store/store/data_quality.proto)';
