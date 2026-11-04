-- A change request is one approve-or-reject batch of typed changes against one
-- target. It is the object a person reviews instead of twenty separate edits, and
-- the row that keeps the decision (state, structured rejection reasons) with the
-- per-item application results.
CREATE TABLE IF NOT EXISTS quality_change_request (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- Stored as QualityChangeRequestState (proto/store/store/data_quality.proto)
    state INT2 NOT NULL DEFAULT 1,
    base_version INT4 NOT NULL DEFAULT 0,
    -- Stored as QualityChangeRequestSource (proto/store/store/data_quality.proto)
    source INT2 NOT NULL DEFAULT 1,
    -- Stored as QualityChangeRequestItems (proto/store/store/data_quality.proto)
    items JSONB NOT NULL DEFAULT '{}',
    -- Stored as QualityChangeRequestItemResults (proto/store/store/data_quality.proto)
    item_results JSONB NOT NULL DEFAULT '{}',
    request_fingerprint TEXT NOT NULL DEFAULT '',
    rationale_codes JSONB NOT NULL DEFAULT '[]',
    decision_note TEXT NOT NULL DEFAULT '',
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    decided_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_change_request_resource
    ON quality_change_request(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_change_request_idempotency
    ON quality_change_request(target_guid, target_type, base_version, source, request_fingerprint);
CREATE INDEX IF NOT EXISTS idx_quality_change_request_target
    ON quality_change_request(target_guid, target_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_change_request_state
    ON quality_change_request(state, created_at DESC);
COMMENT ON TABLE quality_change_request IS 'Approve-or-reject batches of typed quality changes with per-item results';
COMMENT ON COLUMN quality_change_request.items IS 'Stored as QualityChangeRequestItems (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_change_request.item_results IS 'Stored as QualityChangeRequestItemResults (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_change_request_id_seq RESTART WITH 101;
