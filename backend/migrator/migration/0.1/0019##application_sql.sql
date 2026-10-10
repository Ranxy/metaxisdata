
-- application is one application whose statements are reported to the workspace. It is
-- deliberately not a meta registry object: an application has no schema, no columns and
-- no lineage of its own, so it is a plain row rather than a GUID-addressed resource.
CREATE TABLE IF NOT EXISTS application (
    id BIGSERIAL PRIMARY KEY,
    application_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Stopping an application rejects its keys and keeps every row it ever reported.
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_application_id ON application(application_id);
-- The name is the handle a human uses, so two applications may not share one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_name ON application(name);

-- application_sql_api_key is the machine credential an application reports with. One key
-- belongs to exactly one application, and the reported identity comes from the key rather
-- than from the payload: that is what makes it unforgeable. Several keys may be live at
-- once so one can be rotated without a gap.
CREATE TABLE IF NOT EXISTS application_sql_api_key (
    id BIGSERIAL PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES application(application_id) ON DELETE CASCADE,
    -- bcrypt hash; decides acceptance.
    key_hash TEXT NOT NULL,
    -- SHA-256 of the plaintext key, for an indexed lookup. The bcrypt hash still decides
    -- acceptance; without a lookup column every request would bcrypt-compare against
    -- every live key, and bcrypt is deliberately slow.
    key_digest TEXT NOT NULL,
    masked_key TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    -- Swallows one immediate client retry: a repeat of the last accepted report id is
    -- answered with the previous outcome instead of counting twice. Empty means no report
    -- has been accepted yet.
    last_report_id TEXT NOT NULL DEFAULT '',
    -- Per-key counters, which is what this endpoint keeps instead of one audit row per
    -- request. audit_log is kept forever, so auditing machine traffic would make it grow
    -- with request volume rather than with distinct statements.
    request_count BIGINT NOT NULL DEFAULT 0,
    statement_accepted_count BIGINT NOT NULL DEFAULT 0,
    statement_rejected_count BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_sql_api_key_digest ON application_sql_api_key(key_digest);
CREATE INDEX IF NOT EXISTS idx_application_sql_api_key_application ON application_sql_api_key(application_id);

-- application_sql is one distinct reported statement. (instance, database, schema,
-- fingerprint) is its identity, so the same statement reported by two applications is one
-- row and one node; which applications ran it lives in application_sql_observation.
-- instance_resource_id carries a foreign key because the report names an existing
-- instance. database_name deliberately does not: an application commonly starts
-- reporting before the metadata sync has reached its database, and refusing those reports
-- would make the endpoint unusable during a rollout.
CREATE TABLE IF NOT EXISTS application_sql (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    -- Stable internal identifier. The object's GUID is built from it, so a change to the
    -- fingerprint rules can rewrite the fingerprint without detaching lineage or history.
    application_sql_id TEXT NOT NULL,
    instance_resource_id TEXT NOT NULL REFERENCES instance(resource_id),
    database_name TEXT NOT NULL,
    schema_name TEXT NOT NULL DEFAULT '',
    -- The engine of the reporting instance, cached so the runner and the list filters do
    -- not have to join. Refreshed when the instance's engine is re-read.
    engine INT2 NOT NULL,
    -- sha256 of the canonical form. Stored as hex so it can be compared and debugged.
    fingerprint TEXT NOT NULL,
    -- The rule version that produced the fingerprint. A rule change silently splits one
    -- statement into several rows until the re-fingerprint pass has run, so the version is
    -- recorded per row rather than assumed.
    fingerprint_version INT2 NOT NULL,
    -- The parameterized statement: every literal is `?` and comments are gone. This is the
    -- only form of a statement that is ever stored.
    sql_text TEXT NOT NULL,
    -- 0 UNKNOWN, 1 READ, 2 WRITE. Written by the runner once it has analyzed the
    -- statement; it is a filter cache, never the source of the classification.
    kind INT2 NOT NULL DEFAULT 0,
    execution_count BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    content_search TEXT NOT NULL DEFAULT '',
    search_vector TSVECTOR NOT NULL DEFAULT ''::tsvector,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_sql_identity ON application_sql(instance_resource_id, database_name, schema_name, fingerprint);
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_sql_guid ON application_sql(guid);
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_sql_application_sql_id ON application_sql(application_sql_id);
CREATE INDEX IF NOT EXISTS idx_application_sql_last_seen ON application_sql(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_application_sql_search_vector ON application_sql USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_application_sql_scope ON application_sql(instance_resource_id, database_name, schema_name);

-- application_sql_observation is one application's own history with one statement: when
-- it first and last ran it, and how many times. The scope is not repeated here because the
-- statement row already fixes it.
CREATE TABLE IF NOT EXISTS application_sql_observation (
    application_sql_id BIGINT NOT NULL REFERENCES application_sql(id) ON DELETE CASCADE,
    application_id TEXT NOT NULL REFERENCES application(application_id) ON DELETE CASCADE,
    execution_count BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_sql_id, application_id)
);

CREATE INDEX IF NOT EXISTS idx_application_sql_observation_application ON application_sql_observation(application_id);

ALTER SEQUENCE application_id_seq RESTART WITH 101;
ALTER SEQUENCE application_sql_id_seq RESTART WITH 101;
ALTER SEQUENCE application_sql_api_key_id_seq RESTART WITH 101;

