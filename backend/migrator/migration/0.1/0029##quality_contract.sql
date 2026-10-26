-- Contracts add a target-level layer above quality rules: defaults for authoring
-- and a filter every rule on the target is checked with, plus published versions
-- that record what a person promised. The rules stay the mutable source of truth;
-- a contract revision is a snapshot, never a second definition store.
CREATE TABLE IF NOT EXISTS quality_contract (
    id BIGSERIAL PRIMARY KEY,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- Stored as QualityContractSettings (proto/store/store/data_quality.proto)
    settings JSONB NOT NULL DEFAULT '{}',
    -- Newest published version; 0 means the target has never been published.
    published_version INT4 NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_contract_target
    ON quality_contract(target_guid, target_type);

CREATE TABLE IF NOT EXISTS quality_contract_revision (
    id BIGSERIAL PRIMARY KEY,
    contract_id BIGINT NOT NULL REFERENCES quality_contract(id) ON DELETE CASCADE,
    version INT4 NOT NULL,
    -- Stored as QualityContractSnapshot (proto/store/store/data_quality.proto)
    snapshot JSONB NOT NULL DEFAULT '{}',
    check_count INT4 NOT NULL DEFAULT 0,
    added_count INT4 NOT NULL DEFAULT 0,
    modified_count INT4 NOT NULL DEFAULT 0,
    removed_count INT4 NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    published_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_contract_revision_version
    ON quality_contract_revision(contract_id, version);
CREATE INDEX IF NOT EXISTS idx_quality_contract_revision_published
    ON quality_contract_revision(contract_id, published_at DESC);

COMMENT ON TABLE quality_contract IS 'Target-level quality contract settings and newest published version';
COMMENT ON COLUMN quality_contract.settings IS 'Stored as QualityContractSettings (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_contract.published_version IS 'Newest published version; 0 means never published';
COMMENT ON TABLE quality_contract_revision IS 'Append-only published quality contract versions';
COMMENT ON COLUMN quality_contract_revision.snapshot IS 'Stored as QualityContractSnapshot (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_contract_revision.published_by IS 'Principal id of the publisher';

ALTER SEQUENCE quality_contract_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_contract_revision_id_seq RESTART WITH 101;
