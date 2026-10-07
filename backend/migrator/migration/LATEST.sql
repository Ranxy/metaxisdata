-- idp stores generic identity provider.
CREATE TABLE idp (
  id serial PRIMARY KEY,
  resource_id text NOT NULL,
  name text NOT NULL,
  domain text NOT NULL,
  -- Only OAuth2 is implemented; the OIDC/LDAP config messages and enum values
  -- were deleted in the proto surface convergence.
  type text NOT NULL CONSTRAINT idp_type_check CHECK (type IN ('OAUTH2')),
  -- config stores the corresponding configuration of the IdP, which may vary depending on the type of the IdP.
  -- Stored as IdentityProviderConfig (proto/store/store/idp.proto)
  config jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_idp_unique_resource_id ON idp(resource_id);

ALTER SEQUENCE idp_id_seq RESTART WITH 101;

-- principal
CREATE TABLE principal (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    created_at timestamptz NOT NULL DEFAULT now(),
    type text NOT NULL CHECK (type IN ('END_USER', 'SYSTEM_BOT', 'SERVICE_ACCOUNT')),
    name text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    phone text NOT NULL DEFAULT '',
    -- Stored as UserProfile (proto/store/store/user.proto)
    profile jsonb NOT NULL DEFAULT '{}'
);

-- Emails are lower-cased on write. The unique expression index is what rejects
-- a concurrent duplicate registration; soft-deleted rows are excluded so a
-- deleted account does not block reusing its address.
CREATE UNIQUE INDEX idx_principal_unique_email ON principal (LOWER(email)) WHERE deleted = FALSE;

-- The identity provider subjects an account may sign in with. A repeat SSO login
-- resolves against this binding, never against the email column, which is
-- mutable. An account can carry several: one workspace can configure more than
-- one provider, and a person (or a migration) may be enrolled in several.
CREATE TABLE principal_idp_binding (
    principal_id integer NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    idp_resource_id text NOT NULL,
    subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- One account per provider subject: a subject is one person at that provider.
    PRIMARY KEY (idp_resource_id, subject)
);

CREATE INDEX idx_principal_idp_binding_principal ON principal_idp_binding (principal_id);

-- Access tokens revoked before their expiry, so a logout survives a process-local
-- cache a caller can churn and is visible to every replica. The row is keyed by
-- the token's jti and lives until the token itself expires; the maintenance
-- runner prunes expired rows through idx_revoked_token_expires_at.
CREATE TABLE revoked_token (
    jti text PRIMARY KEY,
    -- The token's own expiry: past it the row can never refuse anything.
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_revoked_token_expires_at ON revoked_token (expires_at);

-- Setting
CREATE TABLE setting (
    id serial PRIMARY KEY,
    -- name: AUTH_SECRET, BRANDING_LOGO, WORKSPACE_ID, WORKSPACE_PROFILE,
    -- PASSWORD_RESTRICTION, ENVIRONMENT
    -- Enum: SettingName (proto/store/store/setting.proto)
    name text NOT NULL,
    -- value is deliberately text, not JSONB: it is polymorphic per name.
    -- WORKSPACE_PROFILE, PASSWORD_RESTRICTION and ENVIRONMENT hold the
    -- protojson encoding of their store message, while AUTH_SECRET,
    -- BRANDING_LOGO and WORKSPACE_ID hold a bare string that is not valid
    -- JSON on its own.
    value text NOT NULL
);

CREATE UNIQUE INDEX idx_setting_unique_name ON setting(name);

ALTER SEQUENCE setting_id_seq RESTART WITH 101;


-- Policy
-- policy stores the workspace IAM policy; only the WORKSPACE/IAM row is used.
CREATE TABLE policy (
    id serial PRIMARY KEY,
    enforce boolean NOT NULL DEFAULT TRUE,
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- resource_type: only WORKSPACE is produced now.
    resource_type text NOT NULL,
    -- resource: resource name in format like "environments/{environment}", "projects/{project}", etc.
    resource TEXT NOT NULL,
    -- type: only IAM is produced now.
    type text NOT NULL,
    -- Stored as IamPolicy (proto/store/store/policy.proto) for workspace IAM rows.
    payload jsonb NOT NULL DEFAULT '{}',
    inherit_from_parent boolean NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX idx_policy_unique_resource_type_resource_type ON policy(resource_type, resource, type);

ALTER SEQUENCE policy_id_seq RESTART WITH 101;


CREATE TABLE user_group (
  email text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Stored as GroupPayload (proto/store/store/group.proto)
  payload jsonb NOT NULL DEFAULT '{}'
);


-- Role
-- role stores custom IAM roles: the permission bundles an operator defines
-- alongside the predefined workspaceAdmin/workspaceMember roles, which live in
-- Go (backend/store/predefined_roles.go) and never get a row here.
CREATE TABLE role (
    resource_id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- Stored as RolePermissions (proto/store/store/role.proto)
    permissions jsonb NOT NULL DEFAULT '{}'
);



-- Default system account id is 1.
INSERT INTO principal (id, type, name, email, password_hash) VALUES (1, 'SYSTEM_BOT', 'SYSTEM', 'support@example.com', '');

ALTER SEQUENCE principal_id_seq RESTART WITH 101;


-- Instance
CREATE TABLE instance (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    environment text,
    resource_id text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_instance_unique_resource_id ON instance(resource_id);

ALTER SEQUENCE instance_id_seq RESTART WITH 101;


-- db stores the databases for a particular instance
-- data is synced periodically from the instance
CREATE TABLE db (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    instance text NOT NULL REFERENCES instance(resource_id),
    name text NOT NULL,
    environment text,
    metadata jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_db_unique_instance_name ON db(instance, name);

ALTER SEQUENCE db_id_seq RESTART WITH 101;

-- meta registry for all metadata resources global_id and guid
-- meta_registry_resource holds one row per metadata object; metadata is the
-- protojson form of the proto/store message named by object_type.
--
-- search_text is a stored generated column holding exactly the name/title/
-- comment/userComment fields of the row's inner metadata object, so the search
-- predicate can be a plain `search_text ILIKE '%x%'` served by the trigram GIN
-- index below instead of a sequential scan over a LATERAL jsonb_each.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE OR REPLACE FUNCTION meta_registry_search_text(metadata jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT COALESCE(string_agg(field, ' '), '')
    FROM jsonb_each(metadata) AS e(key, inner_meta)
    CROSS JOIN LATERAL (
        VALUES (inner_meta->>'name'), (inner_meta->>'title'),
               (inner_meta->>'comment'), (inner_meta->>'userComment')
    ) AS fields(field)
    WHERE field IS NOT NULL
$$;

CREATE TABLE meta_registry_resource (
    id serial PRIMARY KEY,
    guid text COLLATE "C" NOT NULL,
    object_type int2 NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    meta_hash bytea,
    search_text text GENERATED ALWAYS AS (meta_registry_search_text(metadata)) STORED
);

CREATE UNIQUE INDEX idx_meta_registry_resource_guid_object_type ON meta_registry_resource(guid,object_type);
-- Serves the lineage analyzer's scan by object_type; the unique index above is
-- led by guid and cannot.
CREATE INDEX idx_meta_registry_resource_object_type ON meta_registry_resource(object_type);
CREATE INDEX idx_meta_registry_resource_search_text ON meta_registry_resource USING GIN (search_text gin_trgm_ops);

CREATE TABLE meta_registry_resource_history (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    object_type INT2 NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    meta_hash BYTEA,
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ
);

CREATE INDEX idx_meta_registry_resource_history_guid_object_type_from ON meta_registry_resource_history(guid, object_type, valid_from DESC);
CREATE UNIQUE INDEX idx_meta_registry_resource_history_open ON meta_registry_resource_history(guid, object_type) WHERE valid_to IS NULL;

ALTER SEQUENCE meta_registry_resource_id_seq RESTART WITH 101;
ALTER SEQUENCE meta_registry_resource_history_id_seq RESTART WITH 101;

-- meta_registry_resource_schema holds the engine's own DDL for a metadata
-- object, keyed by the same (guid, object_type) as meta_registry_resource. It is
-- written by the schema syncer (SHOW CREATE ...) so StarRocks/Doris DDL is
-- exact instead of reconstructed from metadata, and it is deleted in the same
-- transaction as the metadata row. A missing row is normal: the read path falls
-- back to reconstruction.
--
-- schema_hash lets the upsert skip rewriting an unchanged row. Deliberately a
-- separate table: meta_registry_resource is read on every metadata listing and
-- cached, so a wide DDL column there would be loaded (and held in memory) by
-- paths that never need it.
CREATE TABLE meta_registry_resource_schema (
    guid TEXT COLLATE "C" NOT NULL,
    object_type INT2 NOT NULL,
    schema TEXT NOT NULL,
    schema_hash BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guid, object_type)
);


-- manual_sql stores user-maintained SQL definitions and their execution context.
CREATE TABLE manual_sql (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    instance_resource_id TEXT NOT NULL REFERENCES instance(resource_id),
    database_name TEXT NOT NULL,
    schema_name TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    comment TEXT NOT NULL DEFAULT '',
    sql_text TEXT NOT NULL,
    content_search TEXT NOT NULL DEFAULT '',
    search_vector TSVECTOR NOT NULL DEFAULT ''::tsvector,
    created_by TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_manual_sql_database FOREIGN KEY (instance_resource_id, database_name) REFERENCES db(instance, name)
);

CREATE UNIQUE INDEX idx_manual_sql_guid ON manual_sql(guid);
CREATE UNIQUE INDEX idx_manual_sql_scope_name ON manual_sql(instance_resource_id, database_name, name);
CREATE INDEX idx_manual_sql_scope ON manual_sql(instance_resource_id, database_name, schema_name);
CREATE INDEX idx_manual_sql_search_vector ON manual_sql USING GIN(search_vector);

ALTER SEQUENCE manual_sql_id_seq RESTART WITH 101;


-- manual_sql_tag stores normalized tags for exact tag filtering.
CREATE TABLE manual_sql_tag (
    manual_sql_id BIGINT NOT NULL REFERENCES manual_sql(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    tag_norm TEXT NOT NULL,
    PRIMARY KEY (manual_sql_id, tag_norm)
);

CREATE INDEX idx_manual_sql_tag_norm_manual_sql_id ON manual_sql_tag(tag_norm, manual_sql_id);


-- manual_sql_attribute stores normalized key/value attributes for exact filtering.
CREATE TABLE manual_sql_attribute (
    manual_sql_id BIGINT NOT NULL REFERENCES manual_sql(id) ON DELETE CASCADE,
    attr_key TEXT NOT NULL,
    attr_value TEXT NOT NULL DEFAULT '',
    attr_key_norm TEXT NOT NULL,
    attr_value_norm TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (manual_sql_id, attr_key_norm)
);

CREATE INDEX idx_manual_sql_attribute_norm ON manual_sql_attribute(attr_key_norm, attr_value_norm, manual_sql_id);


-- column_lineage stores individual column-level lineage edges.
CREATE TABLE column_lineage (
    id BIGSERIAL PRIMARY KEY,
    meta_guid TEXT COLLATE "C" NOT NULL,
    meta_type INT2 NOT NULL,
    source_guid TEXT COLLATE "C" NOT NULL,
    source_column TEXT COLLATE "C" NOT NULL,
    source_type INT2 NOT NULL DEFAULT 0,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_column TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL DEFAULT 0,
    relation_type INT2 NOT NULL DEFAULT 0,
    transformation JSONB NOT NULL DEFAULT '[]',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_column_lineage_meta ON column_lineage(meta_guid, meta_type);
CREATE INDEX idx_column_lineage_source ON column_lineage(source_guid, source_column);
CREATE INDEX idx_column_lineage_target ON column_lineage(target_guid, target_column);


-- column_lineage_version tracks the analysis state per object for change detection.
CREATE TABLE column_lineage_version (
    meta_guid TEXT COLLATE "C" NOT NULL,
    meta_type INT2 NOT NULL,
    meta_hash BYTEA,
    analyzed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_message TEXT,
    PRIMARY KEY (meta_guid, meta_type)
);


-- external_dataset stores datasets discovered via OpenLineage that are not managed by any instance.
-- The dataset schema stays on the OpenLineage run payload; there is no schema_fields column.
CREATE TABLE external_dataset (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    dataset_type TEXT NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_external_dataset_guid ON external_dataset(guid);
CREATE INDEX idx_external_dataset_ns_name ON external_dataset(namespace, name);

ALTER SEQUENCE external_dataset_id_seq RESTART WITH 101;


-- openlineage_run stores persisted OpenLineage runs and their raw payload. One
-- row holds one run's latest known state: COMPLETE for a run that finished, and
-- START or FAIL for one that is still going or never got there.
CREATE TABLE openlineage_run (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    task_guid TEXT COLLATE "C" NOT NULL,
    run_id TEXT NOT NULL,
    job_namespace TEXT NOT NULL,
    job_name TEXT NOT NULL,
    job_type TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    event_time TIMESTAMPTZ,
    producer TEXT NOT NULL DEFAULT '',
    integration TEXT NOT NULL DEFAULT '',
    processing_type TEXT NOT NULL DEFAULT '',
    parent_job_namespace TEXT NOT NULL DEFAULT '',
    parent_job_name TEXT NOT NULL DEFAULT '',
    parent_run_id TEXT NOT NULL DEFAULT '',
    root_job_namespace TEXT NOT NULL DEFAULT '',
    root_job_name TEXT NOT NULL DEFAULT '',
    root_run_id TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    input_count INT4 NOT NULL DEFAULT 0,
    output_count INT4 NOT NULL DEFAULT 0,
    has_lineage BOOLEAN NOT NULL DEFAULT FALSE,
    raw_payload JSONB NOT NULL,
    -- The Airflow run log URL the payload's airflow facet advertised, already
    -- run through the http(s) whitelist at ingestion. Links are read from here
    -- so a list request never has to parse a raw payload.
    airflow_run_log_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_openlineage_run_guid ON openlineage_run(guid);
CREATE UNIQUE INDEX idx_openlineage_run_unique ON openlineage_run(job_namespace, job_name, job_type, run_id);
CREATE INDEX idx_openlineage_run_event_time ON openlineage_run(event_time DESC NULLS LAST);
CREATE INDEX idx_openlineage_run_job ON openlineage_run(job_namespace, job_name);
CREATE INDEX idx_openlineage_run_task_guid ON openlineage_run(task_guid);

ALTER SEQUENCE openlineage_run_id_seq RESTART WITH 101;


-- openlineage_run_dataset materializes the datasets each persisted run read or
-- wrote, so the dataset pages aggregate in SQL instead of JSON-parsing the raw
-- payload of every recent run. Rows belong to their run: deleting a run (the
-- retention prune, or a redelivery that replaces its row) takes its references
-- with it. schema_fields and column_lineage_fields hold the facet JSON itself,
-- like raw_payload on openlineage_run, not a proto/store message.
CREATE TABLE openlineage_run_dataset (
    id BIGSERIAL PRIMARY KEY,
    run_pk BIGINT NOT NULL REFERENCES openlineage_run(id) ON DELETE CASCADE,
    task_guid TEXT COLLATE "C" NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    -- 'input' for a dataset the run read, 'output' for one it wrote.
    direction TEXT NOT NULL,
    has_column_lineage BOOLEAN NOT NULL DEFAULT FALSE,
    schema_fields JSONB,
    column_lineage_fields JSONB,
    event_time TIMESTAMPTZ,
    integration TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_openlineage_run_dataset_unique ON openlineage_run_dataset(run_pk, namespace, name, direction);
CREATE INDEX idx_openlineage_run_dataset_group ON openlineage_run_dataset(namespace, name, event_time DESC NULLS LAST);
CREATE INDEX idx_openlineage_run_dataset_task ON openlineage_run_dataset(task_guid);
CREATE INDEX idx_openlineage_run_dataset_event_time ON openlineage_run_dataset(event_time DESC NULLS LAST);

ALTER SEQUENCE openlineage_run_dataset_id_seq RESTART WITH 101;


-- openlineage_dataset is the per-dataset aggregate, maintained as runs are
-- ingested so the dataset list, its filter menus and the detail's summary read
-- one small row per dataset instead of grouping every reference row on each
-- request. The row exists while the dataset has references: the store removes it
-- when the last reference goes away. ref_count and column_lineage_ref_count
-- count references, the job counts count distinct task GUIDs per direction, and
-- last_seen is the newest event time of the references.
CREATE TABLE openlineage_dataset (
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    last_seen TIMESTAMPTZ,
    ref_count BIGINT NOT NULL DEFAULT 0,
    source_job_count INTEGER NOT NULL DEFAULT 0,
    target_job_count INTEGER NOT NULL DEFAULT 0,
    column_lineage_ref_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name)
);

CREATE INDEX idx_openlineage_dataset_last_seen ON openlineage_dataset(last_seen DESC NULLS LAST);

-- openlineage_dataset_member holds one row per (dataset, kind, value) with the
-- number of references carrying it, which is what makes the aggregate exact
-- under a redelivery: kind is 'task:input' or 'task:output' (value is the task
-- GUID) or 'integration' or 'source' (value is what the runs reported). The job
-- counts are the number of task rows per direction and the arrays the list
-- shows are read from the value rows, so a reference that goes away takes its
-- contribution back out again. The primary key serves both the array reads and
-- the integration/source filters; the longest value it holds is a task GUID,
-- which ingestion already caps below PostgreSQL's index item size.
CREATE TABLE openlineage_dataset_member (
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    value TEXT NOT NULL,
    ref_count BIGINT NOT NULL,
    PRIMARY KEY (namespace, name, kind, value),
    FOREIGN KEY (namespace, name) REFERENCES openlineage_dataset(namespace, name) ON DELETE CASCADE
);


-- openlineage_task stores aggregated task/job-level views derived from persisted runs.
CREATE TABLE openlineage_task (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    job_namespace TEXT NOT NULL,
    job_name TEXT NOT NULL,
    job_type TEXT NOT NULL DEFAULT '',
    integration TEXT NOT NULL DEFAULT '',
    processing_type TEXT NOT NULL DEFAULT '',
    parent_job_namespace TEXT NOT NULL DEFAULT '',
    parent_job_name TEXT NOT NULL DEFAULT '',
    root_job_namespace TEXT NOT NULL DEFAULT '',
    root_job_name TEXT NOT NULL DEFAULT '',
    latest_run_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    latest_run_id TEXT NOT NULL DEFAULT '',
    latest_event_time TIMESTAMPTZ,
    latest_producer TEXT NOT NULL DEFAULT '',
    latest_source TEXT NOT NULL DEFAULT '',
    run_count INT4 NOT NULL DEFAULT 0,
    lineage_run_count INT4 NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_openlineage_task_guid ON openlineage_task(guid);
CREATE UNIQUE INDEX idx_openlineage_task_unique ON openlineage_task(job_namespace, job_name, job_type);
CREATE INDEX idx_openlineage_task_latest_event_time ON openlineage_task(latest_event_time DESC NULLS LAST);
CREATE INDEX idx_openlineage_task_job ON openlineage_task(job_namespace, job_name, job_type);

ALTER SEQUENCE openlineage_task_id_seq RESTART WITH 101;


-- namespace_mapping maps OpenLineage namespaces to internal instances for auto-resolution.
CREATE TABLE namespace_mapping (
    id BIGSERIAL PRIMARY KEY,
    namespace TEXT NOT NULL,
    instance_resource_id TEXT NOT NULL,
    database_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_namespace_mapping_namespace ON namespace_mapping(namespace);

ALTER SEQUENCE namespace_mapping_id_seq RESTART WITH 101;


-- openlineage_api_key stores hashed API keys for authenticating OpenLineage event submissions.
-- key_digest is a SHA-256 lookup digest so validation does not have to bcrypt
-- every row; scope_namespace restricts a key to one OpenLineage namespace
-- ('' means unscoped).
CREATE TABLE openlineage_api_key (
    id BIGSERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL,
    masked_key TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    key_digest TEXT,
    scope_namespace TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_openlineage_api_key_hash ON openlineage_api_key(key_hash);
CREATE UNIQUE INDEX idx_openlineage_api_key_digest ON openlineage_api_key(key_digest) WHERE key_digest IS NOT NULL;

ALTER SEQUENCE openlineage_api_key_id_seq RESTART WITH 101;


-- audit_log stores append-only audit events for auditable API calls.
CREATE TABLE audit_log (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);
CREATE INDEX idx_audit_log_payload_parent ON audit_log((payload->>'parent'));
CREATE INDEX idx_audit_log_payload_method ON audit_log((payload->>'method'));
CREATE INDEX idx_audit_log_payload_resource ON audit_log((payload->>'resource'));
CREATE INDEX idx_audit_log_payload_user ON audit_log((payload->>'user'));
CREATE INDEX idx_audit_log_payload_severity ON audit_log((payload->>'severity'));


-- llm_provider_profile stores user-managed LLM provider profiles.
CREATE TABLE llm_provider_profile (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT UNIQUE NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER SEQUENCE llm_provider_profile_id_seq RESTART WITH 101;


-- explain_sql_cache stores cached LLM explanations for SQL objects.
CREATE TABLE explain_sql_cache (
    id BIGSERIAL PRIMARY KEY,
    cache_key TEXT UNIQUE NOT NULL,
    cache_type INT NOT NULL DEFAULT 0,
    meta_guid TEXT NOT NULL DEFAULT '',
    -- scope is the instance/object prefix the explanation was generated for;
    -- it is part of cache_key and stored separately for inspection.
    scope TEXT NOT NULL DEFAULT '',
    sql_text TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    explanation_json JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER SEQUENCE explain_sql_cache_id_seq RESTART WITH 101;

-- The maintenance runner prunes expired rows by created_at.
CREATE INDEX idx_explain_sql_cache_created_at ON explain_sql_cache(created_at);


-- llm_debug_log stores full LLM request/response bodies when RuntimeDebug is enabled.
CREATE TABLE llm_debug_log (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    request_body TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER SEQUENCE llm_debug_log_id_seq RESTART WITH 101;

-- The maintenance runner prunes old debug entries by created_at.
CREATE INDEX idx_llm_debug_log_created_at ON llm_debug_log(created_at);


-- oauth_client stores RFC 7591 dynamic client registrations for the MCP OAuth
-- 2.1 authorization server. A registration is a governance object -- "which
-- application has connected to this workspace" -- so it survives a restart,
-- unlike the process-local pending authorization requests.
--
-- client_id is the unique public identifier; the surrogate id is internal. The
-- column names follow the table conventions here: created_at, and last_used_at
-- as in openlineage_api_key. token_endpoint_auth_method stores what the
-- registration requested; only public clients ('none') are accepted today, and
-- the endpoint rather than a CHECK constraint enforces that.
--
-- redirect_uris is a JSON array of strings. It is deliberately not bound to a
-- proto/store message (see the column comment at the end of this file): there
-- is no message for a bare string list, and the value is server-built like
-- explain_sql_cache.explanation_json.
CREATE TABLE IF NOT EXISTS oauth_client (
    id BIGSERIAL PRIMARY KEY,
    client_id TEXT NOT NULL,
    client_name TEXT NOT NULL DEFAULT '',
    redirect_uris JSONB NOT NULL,
    token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_client_id ON oauth_client(client_id);


-- The two refresh-token tables. Both store only the SHA-256 hex digest of the
-- opaque token, never the plaintext, and both are single-use: a refresh
-- atomically deletes the row it consumes (DELETE ... RETURNING) and inserts a
-- replacement, so concurrent refreshes race and a replayed token resolves to no
-- row. user_id references principal(id) because an account is identified by its
-- principal id, not by its mutable address; deactivation is a soft delete, so
-- the refresh handlers re-read the principal and refuse a deactivated one or a
-- grant older than the last password change.
--
-- oauth_refresh_token carries the MCP grant across a refresh: resource pins it
-- to the `<external_url>/mcp` the user consented to, and scope is carried
-- forward verbatim so a refresh re-issues the grant as consented.
CREATE TABLE IF NOT EXISTS oauth_refresh_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    client_id TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    resource TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_refresh_token_hash ON oauth_refresh_token(token_hash);

-- Logout revokes every grant a user holds for one client.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_client ON oauth_refresh_token(user_id, client_id);

-- The maintenance runner's TTL prune.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_expires_at ON oauth_refresh_token(expires_at);

-- web_refresh_token is the SPA session's rotation state. It carries only the
-- principal and the absolute expiry: the workspace is single tenant, and a
-- refreshed access token is minted from the reloaded principal.
CREATE TABLE IF NOT EXISTS web_refresh_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_web_refresh_token_hash ON web_refresh_token(token_hash);

-- The maintenance runner's TTL prune.
CREATE INDEX IF NOT EXISTS idx_web_refresh_token_expires_at ON web_refresh_token(expires_at);


-- schema_migration_history records every applied schema version, one row per
-- migration (and one baseline row for a fresh install). It is the version
-- ledger the migrator (backend/migrator) reads to decide which incremental
-- files under migration/{MAJOR.MINOR}/ are still pending. Created by this file
-- on fresh installs and by the migrator when adopting a pre-framework
-- database; never written by application code.
CREATE TABLE IF NOT EXISTS schema_migration_history (
    id BIGSERIAL PRIMARY KEY,
    version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_schema_migration_history_unique_version
    ON schema_migration_history (version);

-- JSONB → proto/store message bindings. AGENTS.md treats these column comments
-- as the contract between a JSONB column and the store message it holds, so the
-- cumulative schema carries them; migration/0.1/0009##jsonb_column_comments.sql
-- applies the same statements to existing deployments.
COMMENT ON COLUMN instance.metadata IS 'Stored as Instance (proto/store/store/instance.proto)';
COMMENT ON COLUMN db.metadata IS 'Stored as DatabaseMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN meta_registry_resource.metadata IS 'Stored as StoredMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN meta_registry_resource_history.metadata IS 'Stored as StoredMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN audit_log.payload IS 'Stored as AuditLog (proto/store/store/audit_log.proto)';
COMMENT ON COLUMN llm_provider_profile.metadata IS 'Stored as LlmProviderProfile (proto/store/store/llm.proto)';
COMMENT ON COLUMN explain_sql_cache.explanation_json IS 'Server-built JSON ({summary, sections}); not a proto message.';
COMMENT ON COLUMN oauth_client.redirect_uris IS 'Server-built JSON (array of strings); not a proto message.';
COMMENT ON COLUMN openlineage_api_key.key_digest IS 'SHA-256 hex digest of the plaintext key for lookup; key_hash (bcrypt) decides acceptance.';
COMMENT ON COLUMN openlineage_api_key.scope_namespace IS 'OpenLineage namespace this key is scoped to; empty means unrestricted.';
