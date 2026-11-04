-- A memory is durable knowledge with its evidence and a correction path: an asset
-- fact may be written automatically, while a decision or an event needs a person's
-- confirmation before it counts. Nothing is silently deleted: superseded and
-- expired rows stay for the correction path, and a delete is a soft delete.
CREATE TABLE IF NOT EXISTS quality_memory (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    -- Stored as QualityMemoryKind (proto/store/store/data_quality.proto)
    kind INT2 NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    statement TEXT NOT NULL DEFAULT '',
    -- Stored as QualityMemorySource (proto/store/store/data_quality.proto)
    source INT2 NOT NULL DEFAULT 0,
    -- Stored as QualityMemoryStatus (proto/store/store/data_quality.proto)
    status INT2 NOT NULL DEFAULT 1,
    -- Stored as QualityMemoryEvidenceList (proto/store/store/data_quality.proto)
    evidence JSONB NOT NULL DEFAULT '{}',
    domain_ref TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    confirmed_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    confirmed_at TIMESTAMPTZ,
    superseded_by TEXT NOT NULL DEFAULT '',
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_memory_resource
    ON quality_memory(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_memory_subject
    ON quality_memory(kind, subject, created_at DESC) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_quality_memory_status
    ON quality_memory(status, created_at DESC) WHERE deleted = FALSE;
COMMENT ON TABLE quality_memory IS 'Durable knowledge with evidence and a correction path: asset facts, decisions and events';
COMMENT ON COLUMN quality_memory.kind IS 'Stored as QualityMemoryKind (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.source IS 'Stored as QualityMemorySource (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.status IS 'Stored as QualityMemoryStatus (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_memory.evidence IS 'Stored as QualityMemoryEvidenceList (proto/store/store/data_quality.proto); references only, never sample payloads';
ALTER SEQUENCE quality_memory_id_seq RESTART WITH 101;
