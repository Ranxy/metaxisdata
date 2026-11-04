-- An investigation is one agent's work product on one event: hypotheses, deterministic
-- evidence references, a verdict and its cost. It is not a second verdict engine: it
-- never changes a check's result or an issue's state, and the evidence is references,
-- never sample payloads.
CREATE TABLE IF NOT EXISTS quality_investigation (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- Stored as QualityInvestigationTrigger (proto/store/store/data_quality.proto)
    trigger INT2 NOT NULL DEFAULT 0,
    trigger_reference TEXT NOT NULL DEFAULT '',
    -- Stored as QualityInvestigationStatus (proto/store/store/data_quality.proto)
    status INT2 NOT NULL DEFAULT 1,
    -- Stored as QualityInvestigationVerdict (proto/store/store/data_quality.proto)
    verdict INT2 NOT NULL DEFAULT 0,
    summary TEXT NOT NULL DEFAULT '',
    -- Stored as QualityInvestigationHypothesisList (proto/store/store/data_quality.proto)
    hypotheses JSONB NOT NULL DEFAULT '{}',
    -- Stored as QualityInvestigationEvidenceList (proto/store/store/data_quality.proto)
    evidence JSONB NOT NULL DEFAULT '{}',
    -- Stored as QualityInvestigationActionRefs (proto/store/store/data_quality.proto)
    action_refs JSONB NOT NULL DEFAULT '{}',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    prompt_tokens INT8 NOT NULL DEFAULT 0,
    completion_tokens INT8 NOT NULL DEFAULT 0,
    failure_reason TEXT NOT NULL DEFAULT '',
    request_fingerprint TEXT NOT NULL DEFAULT '',
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_investigation_resource
    ON quality_investigation(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_investigation_idempotency
    ON quality_investigation(target_guid, target_type, request_fingerprint)
    WHERE request_fingerprint <> '';
CREATE INDEX IF NOT EXISTS idx_quality_investigation_target
    ON quality_investigation(target_guid, target_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_investigation_status
    ON quality_investigation(status, created_at DESC);
COMMENT ON TABLE quality_investigation IS 'Agent work products on one event: hypotheses, evidence references, verdict and cost';
COMMENT ON COLUMN quality_investigation.hypotheses IS 'Stored as QualityInvestigationHypothesisList (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_investigation.evidence IS 'Stored as QualityInvestigationEvidenceList (proto/store/store/data_quality.proto); references only, never sample payloads';
COMMENT ON COLUMN quality_investigation.action_refs IS 'Stored as QualityInvestigationActionRefs (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_investigation.trigger IS 'Stored as QualityInvestigationTrigger (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_investigation.status IS 'Stored as QualityInvestigationStatus (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_investigation.verdict IS 'Stored as QualityInvestigationVerdict (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_investigation_id_seq RESTART WITH 101;
