-- Data quality: the rule registry and its immutable revision history, the
-- execution runs and per-rule results, the issue ledger, column profiles and
-- semantic labels, the suggestion review queue, and the validated LLM result
-- cache. The same DDL is appended to migration/LATEST.sql, which serves fresh
-- installs; existing deployments get it from this incremental.

-- quality_rule is the mutable head of a rule: display metadata, current config,
-- target binding, schedule and state. The immutable definition history lives in
-- quality_rule_revision; both are written in one transaction on every edit.
CREATE TABLE IF NOT EXISTS quality_rule (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: the resource name carries this string, not the row id,
    -- and it is never reused.
    resource_id TEXT COLLATE "C" NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    dimension INT2 NOT NULL,
    rule_type INT2 NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    target_path JSONB NOT NULL DEFAULT '{}',
    -- The target's meta_hash when the rule was saved. A different current hash
    -- means the target's schema changed and the rule may need review.
    target_meta_hash BYTEA,
    config JSONB NOT NULL DEFAULT '{}',
    severity INT2 NOT NULL,
    -- 1 ENABLED, 2 PAUSED, 3 ARCHIVED. Only ENABLED rules are scheduled.
    state INT2 NOT NULL DEFAULT 1,
    owner_id INT4 REFERENCES principal(id) ON DELETE SET NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    schedule_interval_seconds INT4 NOT NULL,
    -- When the scheduler next considers this rule due; NULL means not scheduled.
    next_run_at TIMESTAMPTZ,
    -- Rule-level mute of issue notifications. A muted rule is still executed and
    -- its results are still recorded; only the notifications stop.
    mute_until TIMESTAMPTZ,
    revision INT4 NOT NULL DEFAULT 1,
    created_by INT4,
    updated_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_rule_resource_id ON quality_rule(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_rule_due ON quality_rule(next_run_at)
    WHERE state = 1 AND deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_quality_rule_target ON quality_rule(target_guid, target_type);
CREATE INDEX IF NOT EXISTS idx_quality_rule_owner ON quality_rule(owner_id);
CREATE INDEX IF NOT EXISTS idx_quality_rule_tags ON quality_rule USING GIN(tags);

-- quality_rule_revision is append-only: every semantic edit snapshots target,
-- type, dimension, severity and config, so a result can always be explained by
-- the definition that produced it.
CREATE TABLE IF NOT EXISTS quality_rule_revision (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES quality_rule(id),
    revision INT4 NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    target_path JSONB NOT NULL DEFAULT '{}',
    target_meta_hash BYTEA,
    dimension INT2 NOT NULL,
    rule_type INT2 NOT NULL,
    severity INT2 NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_rule_revision_rule_rev ON quality_rule_revision(rule_id, revision);

-- quality_run is one execution batch: manually selected rules, or the rules of
-- one instance that became due in one scheduler tick.
CREATE TABLE IF NOT EXISTS quality_run (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    instance_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    -- 1 MANUAL, 2 SCHEDULED.
    trigger INT2 NOT NULL,
    -- Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED (a run whose
    -- process was lost across a restart).
    status INT2 NOT NULL,
    -- Rollup of the run's results: the worst QualityStatus any result reported
    -- (1 PASS ... 6 NOT_EVALUATED). NULL while the run has not finished, which
    -- is what tells a queued run apart from a finished one that evaluated
    -- nothing.
    outcome INT2,
    counts JSONB NOT NULL DEFAULT '{}',
    -- Principal id of the user who started the run; NULL for a scheduled run.
    initiator_id INT4,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_run_resource_id ON quality_run(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_run_started_at ON quality_run(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_run_instance_started ON quality_run(instance_resource_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_run_status ON quality_run(status);
-- The run list can filter by the quality rollup, which only completed runs have.
CREATE INDEX IF NOT EXISTS idx_quality_run_outcome ON quality_run(outcome) WHERE outcome IS NOT NULL;

-- quality_rule_result is one rule's outcome in one run. Rows are insert-only;
-- retention deletes them with their run.
CREATE TABLE IF NOT EXISTS quality_rule_result (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES quality_run(id) ON DELETE CASCADE,
    rule_id BIGINT NOT NULL REFERENCES quality_rule(id),
    -- The revision that produced this result, so editing a rule never rewrites
    -- its history.
    rule_revision INT4 NOT NULL,
    rule_engine_version TEXT NOT NULL DEFAULT '',
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    status INT2 NOT NULL,
    -- NULL when the check could not produce a value (ERROR or NOT_EVALUATED).
    observed_value DOUBLE PRECISION,
    -- Unit of observed_value: 1 COUNT, 2 RATIO, 3 SECONDS, 4 TIMESTAMP,
    -- 5 NUMBER.
    observed_kind INT2 NOT NULL DEFAULT 0,
    thresholds JSONB NOT NULL DEFAULT '{}',
    failed_rows BIGINT NOT NULL DEFAULT 0,
    total_rows BIGINT NOT NULL DEFAULT 0,
    sampled BOOLEAN NOT NULL DEFAULT FALSE,
    sample_rows BIGINT NOT NULL DEFAULT 0,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    sample JSONB NOT NULL DEFAULT '{}',
    executed_sql TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INT4 NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_quality_rule_result_run ON quality_rule_result(run_id);
CREATE INDEX IF NOT EXISTS idx_quality_rule_result_rule_time ON quality_rule_result(rule_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_rule_result_target_time ON quality_rule_result(target_guid, target_type, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_rule_result_started_at ON quality_rule_result(started_at);

-- quality_issue is the persistent problem for one rule. The partial unique index
-- is what makes "at most one unresolved issue per rule" true under concurrent
-- runs, retries and notification writes.
CREATE TABLE IF NOT EXISTS quality_issue (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    rule_id BIGINT NOT NULL REFERENCES quality_rule(id),
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- 1 OPEN, 2 ACKNOWLEDGED, 3 RESOLVED. The partial unique index below uses
    -- "<> 3" so at most one unresolved issue exists per rule.
    status INT2 NOT NULL,
    severity INT2 NOT NULL,
    first_seen_run_id BIGINT,
    last_seen_run_id BIGINT,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    resolution_reason INT2,
    resolution_note TEXT NOT NULL DEFAULT '',
    acknowledged_by INT4,
    acknowledged_at TIMESTAMPTZ,
    muted_until TIMESTAMPTZ,
    occurrence_count INT4 NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_issue_resource_id ON quality_issue(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_issue_open_rule ON quality_issue(rule_id) WHERE status <> 3;
CREATE INDEX IF NOT EXISTS idx_quality_issue_status_severity ON quality_issue(status, severity);
CREATE INDEX IF NOT EXISTS idx_quality_issue_target ON quality_issue(target_guid, target_type);

-- column_profile caches the deterministic statistics of the latest profiling of
-- one column. It is an upsert (one row per column), pruned after 7 days.
CREATE TABLE IF NOT EXISTS column_profile (
    id BIGSERIAL PRIMARY KEY,
    table_guid TEXT COLLATE "C" NOT NULL,
    table_type INT2 NOT NULL,
    column_name TEXT COLLATE "C" NOT NULL,
    profile JSONB NOT NULL DEFAULT '{}',
    sample_method TEXT NOT NULL DEFAULT '',
    sample_rows BIGINT NOT NULL DEFAULT 0,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    engine_version TEXT NOT NULL DEFAULT '',
    profiled_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_column_profile_target ON column_profile(table_guid, table_type, column_name);
CREATE INDEX IF NOT EXISTS idx_column_profile_profiled_at ON column_profile(profiled_at);

-- column_semantic_label is product data, not synced metadata: it must survive
-- schema sync and is keyed by the table GUID plus the column name (views/MVs
-- have no COLUMN registry row). Only ACCEPTED/manual labels are stored here;
-- pending suggestions live in quality_suggestion.
CREATE TABLE IF NOT EXISTS column_semantic_label (
    id BIGSERIAL PRIMARY KEY,
    table_guid TEXT COLLATE "C" NOT NULL,
    table_type INT2 NOT NULL,
    column_name TEXT COLLATE "C" NOT NULL,
    label INT2 NOT NULL,
    -- 1 MANUAL, 2 LLM.
    source INT2 NOT NULL,
    status INT2 NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence JSONB NOT NULL DEFAULT '{}',
    accepted_by INT4,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_column_semantic_label_target ON column_semantic_label(table_guid, table_type, column_name);
CREATE INDEX IF NOT EXISTS idx_column_semantic_label_label ON column_semantic_label(label);

-- quality_suggestion is the review queue for L1 natural-language drafts and L3
-- profile-driven drafts. Nothing here is a rule: acceptance edits and saves a
-- rule through the normal rule path.
CREATE TABLE IF NOT EXISTS quality_suggestion (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    -- 1 RULE_DRAFT, 2 LABEL.
    kind INT2 NOT NULL,
    -- 1 NATURAL_LANGUAGE, 2 PROFILE.
    source INT2 NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    -- 1 PENDING, 2 ACCEPTED, 3 REJECTED.
    status INT2 NOT NULL DEFAULT 1,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    created_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_by INT4,
    decided_at TIMESTAMPTZ,
    decision_note TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_resource_id ON quality_suggestion(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_status_time ON quality_suggestion(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_target ON quality_suggestion(target_guid, target_type);

-- data_quality_llm_cache keeps validated LLM outputs. It is separate from
-- explain_sql_cache because its invalidation axis is rule revision / prompt
-- version / data snapshot, not SQL text.
CREATE TABLE IF NOT EXISTS data_quality_llm_cache (
    cache_key TEXT COLLATE "C" PRIMARY KEY,
    kind INT2 NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    result_json JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_data_quality_llm_cache_created_at ON data_quality_llm_cache(created_at);

-- JSONB → proto/store message bindings. AGENTS.md treats a column comment as the
-- contract for what protojson writes into the column, so each JSONB column here
-- names its message.
COMMENT ON COLUMN quality_rule.target_path IS 'Stored as QualityTargetPath (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_rule.config IS 'Stored as QualityRuleConfig (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_rule_revision.target_path IS 'Stored as QualityTargetPath (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_rule_revision.config IS 'Stored as QualityRuleConfig (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_run.counts IS 'Stored as QualityRunCounts (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_rule_result.thresholds IS 'Stored as QualityThresholds (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_rule_result.sample IS 'Stored as QualityFailureSamples (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN column_profile.profile IS 'Stored as ColumnProfile (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN column_semantic_label.evidence IS 'Stored as SemanticLabelEvidence (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_suggestion.payload IS 'Stored as QualitySuggestionPayload (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN data_quality_llm_cache.result_json IS 'Stored as DataQualityLLMResult (proto/store/store/data_quality.proto)';

ALTER SEQUENCE quality_rule_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_rule_revision_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_run_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_rule_result_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_issue_id_seq RESTART WITH 101;
ALTER SEQUENCE column_profile_id_seq RESTART WITH 101;
ALTER SEQUENCE column_semantic_label_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_suggestion_id_seq RESTART WITH 101;
