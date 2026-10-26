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
    profile jsonb NOT NULL DEFAULT '{}',
    -- When this user last opened the inbox. The bell's badge counts unread
    -- messages created after this instant, so opening the inbox clears it
    -- without making anything read. NULL means the inbox was never opened,
    -- which reads as "everything unread is new".
    notification_seen_at timestamptz
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
    -- PASSWORD_RESTRICTION, ENVIRONMENT, ENCRYPTION_KEY
    -- Enum: SettingName (proto/store/store/setting.proto)
    name text NOT NULL,
    -- value is deliberately text, not JSONB: it is polymorphic per name.
    -- WORKSPACE_PROFILE, PASSWORD_RESTRICTION and ENVIRONMENT hold the
    -- protojson encoding of their store message, while AUTH_SECRET,
    -- BRANDING_LOGO and WORKSPACE_ID hold a bare string that is not valid
    -- JSON on its own. ENCRYPTION_KEY holds a bare base64 data key, or a
    -- "v1:"-prefixed AES-256-GCM ciphertext of it when the deployment
    -- configured METAXISDATA_ENCRYPTION_KEY.
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
-- Serves the lineage analyzer's lookup of the name a statement wrote against the
-- spelling the sync registered. guid is COLLATE "C", which lowers ASCII alone, so
-- the folding names the database default collation explicitly.
CREATE INDEX idx_meta_registry_resource_guid_folded ON meta_registry_resource (lower(guid COLLATE "default"));

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
-- One row per distinct edge: two writers replacing the same object's edges at once
-- (the analyzer and OpenLineage revalidation) would otherwise duplicate them. The
-- insertion uses ON CONFLICT DO NOTHING; transformation is not part of the identity.
CREATE UNIQUE INDEX idx_column_lineage_edge ON column_lineage(
    meta_guid,
    meta_type,
    source_guid,
    source_column,
    target_guid,
    target_column,
    relation_type
);


-- column_lineage_version tracks the analysis state per object for change detection.
CREATE TABLE column_lineage_version (
    meta_guid TEXT COLLATE "C" NOT NULL,
    meta_type INT2 NOT NULL,
    meta_hash BYTEA,
    analyzed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_message TEXT,
    analyzer_version TEXT NOT NULL DEFAULT '',
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
    -- The feature that made the call (for example explain_sql or dq_rule_draft).
    kind TEXT NOT NULL DEFAULT '',
    -- The caller's correlation id, echoed by the server log and the failure diagnostics.
    correlation_id TEXT NOT NULL DEFAULT '',
    -- The classifier code when the call failed; empty for a successful call.
    error_code TEXT NOT NULL DEFAULT '',
    request_body TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER SEQUENCE llm_debug_log_id_seq RESTART WITH 101;

-- The maintenance runner prunes old debug entries by created_at.
CREATE INDEX idx_llm_debug_log_created_at ON llm_debug_log(created_at);
-- A failure's diagnostic carries only the correlation id; look the bodies up by it.
CREATE INDEX idx_llm_debug_log_correlation_id ON llm_debug_log(correlation_id);


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
-- opaque token, never the plaintext, and both are single-use, so concurrent
-- refreshes race and a replayed token resolves to no row. The MCP grant rotates
-- in place (UPDATE ... RETURNING id) because its row id is the connection's
-- stable handle; the web session consumes its row with DELETE ... RETURNING and
-- inserts a replacement. user_id references principal(id) because an account is
-- identified by its principal id, not by its mutable address; deactivation is a
-- soft delete, so the refresh handlers re-read the principal and refuse a
-- deactivated one or a grant older than the last password change.
--
-- oauth_refresh_token is also the durable record of one connected app: one
-- user's authorization of one MCP client. resource pins it to the
-- `<external_url>/mcp` the user consented to, and scope is carried forward
-- verbatim so a refresh re-issues the grant as consented. last_tool_call_at is
-- per authorization (the MCP tool path updates it, throttled); revoked_at is a
-- soft revocation the management page reads and no path may clear (authorizing
-- again creates a new row); revoked_by is who revoked it, or NULL when the
-- system ended it (a refresh refused for a gone/deactivated principal or a
-- password change).
CREATE TABLE IF NOT EXISTS oauth_refresh_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    client_id TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    resource TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_tool_call_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by INTEGER REFERENCES principal(id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_refresh_token_hash ON oauth_refresh_token(token_hash);

-- Logout revokes every grant a user holds for one client.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_client ON oauth_refresh_token(user_id, client_id);

-- The maintenance runner's TTL prune.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_expires_at ON oauth_refresh_token(expires_at);

-- The connected-apps list is newest first, scoped by user for self-service.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_created ON oauth_refresh_token(user_id, created_at DESC);

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


-- notification is one in-app message for one user: the outcome of a sync
-- operation they asked for, or an ingestion failure the workspace
-- administrators need to see. It is personal data, so unlike every other table
-- its reads are scoped by recipient_id rather than by the workspace.
CREATE TABLE IF NOT EXISTS notification (
    id BIGSERIAL PRIMARY KEY,
    recipient_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL means unread.
    read_at TIMESTAMPTZ,
    -- Deduplication key of a background notification, empty when the message is
    -- not deduplicated. Format: <event>:<target>:<bucket>.
    dedupe_key TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'
);

-- The inbox lists one user's messages newest first.
CREATE INDEX IF NOT EXISTS idx_notification_recipient_created_at
    ON notification(recipient_id, created_at DESC, id DESC);

-- The unread badge counts without scanning read rows.
CREATE INDEX IF NOT EXISTS idx_notification_recipient_unread
    ON notification(recipient_id) WHERE read_at IS NULL;

-- Suppresses a repeated background notification within its window. Enforced
-- here rather than in process memory so that replicas and restarts share it.
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_dedupe
    ON notification(recipient_id, dedupe_key) WHERE dedupe_key <> '';


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
COMMENT ON COLUMN notification.payload IS 'Stored as Notification (proto/store/store/notification.proto)';
COMMENT ON COLUMN llm_provider_profile.metadata IS 'Stored as LlmProviderProfile (proto/store/store/llm.proto)';
COMMENT ON COLUMN explain_sql_cache.explanation_json IS 'Server-built JSON ({summary, sections}); not a proto message.';
COMMENT ON COLUMN oauth_client.redirect_uris IS 'Server-built JSON (array of strings); not a proto message.';
COMMENT ON COLUMN openlineage_api_key.key_digest IS 'SHA-256 hex digest of the plaintext key for lookup; key_hash (bcrypt) decides acceptance.';
COMMENT ON COLUMN openlineage_api_key.scope_namespace IS 'OpenLineage namespace this key is scoped to; empty means unrestricted.';

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
    -- Swallows one immediate client retry: a repeat of the last report id that landed whole
    -- is answered with the previous outcome instead of counting twice. A report with a
    -- refusal is not remembered here, because the application has to be able to send that
    -- report again. Empty means no report has landed whole yet.
    last_report_id TEXT NOT NULL DEFAULT '',
    -- Per-key counters, which is what this endpoint keeps instead of one audit row per
    -- request. audit_log is kept forever, so auditing machine traffic would make it grow
    -- with request volume rather than with distinct statements.
    request_count BIGINT NOT NULL DEFAULT 0,
    -- Statements, not entries: an entry carrying a script counts once per statement in it.
    -- The response counts entries, because its results are one per entry.
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
    -- not have to join. It follows the reporting instance's next report; nothing re-reads an
    -- instance's engine on its own.
    engine INT2 NOT NULL,
    -- sha256 of the canonical form. Stored as hex so it can be compared and debugged.
    fingerprint TEXT NOT NULL,
    -- The rule version that produced the fingerprint. A rule change splits one statement
    -- into rows fingerprinted under two versions, and this column is how a deployment finds
    -- out; reconciling them is that change's business, not this schema's.
    fingerprint_version INT2 NOT NULL,
    -- The parameterized statement: every literal is `?` and comments are gone. This is the
    -- only form of a statement that is ever stored.
    sql_text TEXT NOT NULL,
    -- 0 UNKNOWN, 1 READ, 2 WRITE. Written by the runner before the analysis writes the
    -- statement's edges; it is a filter cache, never the source of the classification, so a
    -- failed edge write leaves it set until the next analysis rewrites both.
    kind INT2 NOT NULL DEFAULT 0,
    execution_count BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
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

-- quality_rule is the mutable head of a rule: display metadata, current config,
-- target binding and state. The immutable definition history lives in
-- quality_rule_revision; both are written in one transaction on every edit.
-- Scheduling is not a rule property: quality_target_schedule owns the timer for
-- every enabled rule of one target.
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
    -- 1 ENABLED, 2 PAUSED, 3 ARCHIVED. Only ENABLED rules run on the target schedule.
    state INT2 NOT NULL DEFAULT 1,
    owner_id INT4 REFERENCES principal(id) ON DELETE SET NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
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
CREATE INDEX IF NOT EXISTS idx_quality_rule_target ON quality_rule(target_guid, target_type);
CREATE INDEX IF NOT EXISTS idx_quality_rule_owner ON quality_rule(owner_id);
CREATE INDEX IF NOT EXISTS idx_quality_rule_tags ON quality_rule USING GIN(tags);

-- quality_target_schedule is the one timer for a target's checks. Scheduling is
-- per target, not per rule: when next_run_at comes due, every enabled rule of
-- the target is planned into one dispatch batch. A target row is created with
-- the first rule and stays while the target has rules; next_run_at is NULL while
-- the target has no enabled rule.
CREATE TABLE IF NOT EXISTS quality_target_schedule (
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- 15 minutes..7 days. The API validates the range; the runner keeps a minimum
    -- backstop for a malformed row.
    schedule_interval_seconds INT4 NOT NULL,
    -- When the scheduler next considers the target due; NULL means its checks are
    -- all paused or archived.
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (target_guid, target_type)
);
CREATE INDEX IF NOT EXISTS idx_quality_target_schedule_due ON quality_target_schedule(next_run_at)
    WHERE next_run_at IS NOT NULL;

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

-- quality_run is an internal dispatch batch that belongs to one target: the
-- target's checks that became due in one scheduler tick, or a manual selection
-- grouped by target. It is never exposed through the API as a write path; it
-- exists to admit one connection lease per instance, aggregate a storm
-- notification, and cascade retention. target_path is the display snapshot the
-- plan used, so the batch stays readable if the target is later orphaned.
CREATE TABLE IF NOT EXISTS quality_run (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    instance_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    target_path JSONB NOT NULL DEFAULT '{}',
    -- 1 MANUAL, 2 SCHEDULED.
    trigger INT2 NOT NULL,
    -- Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED (a batch whose
    -- process was lost across a restart).
    status INT2 NOT NULL,
    -- Rollup of the batch's terminal executions: the worst QualityStatus any of
    -- them reported (1 PASS ... 6 NOT_EVALUATED). NULL while the batch has not
    -- finished.
    outcome INT2,
    counts JSONB NOT NULL DEFAULT '{}',
    -- Principal id of the user who started the batch; NULL for a scheduled one.
    initiator_id INT4,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_run_resource_id ON quality_run(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_run_started_at ON quality_run(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_run_instance_started ON quality_run(instance_resource_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_run_target_started ON quality_run(target_guid, target_type, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_run_status ON quality_run(status);
CREATE INDEX IF NOT EXISTS idx_quality_run_outcome ON quality_run(outcome) WHERE outcome IS NOT NULL;

-- quality_execution is one rule checked once: the durable, user-facing unit of
-- the data quality engine. A batch plans one row per rule (PENDING); the runner
-- marks it RUNNING, then writes the verdict and evidence and marks it COMPLETED.
CREATE TABLE IF NOT EXISTS quality_execution (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES quality_run(id) ON DELETE CASCADE,
    rule_id BIGINT NOT NULL REFERENCES quality_rule(id),
    -- The revision that produced this execution, so editing a rule never
    -- rewrites its history.
    rule_revision INT4 NOT NULL,
    rule_engine_version TEXT NOT NULL DEFAULT '',
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    target_path JSONB NOT NULL DEFAULT '{}',
    -- The instance the target lives in; empty for a target that could not be
    -- attributed to one.
    instance_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    -- 1 MANUAL, 2 SCHEDULED.
    trigger INT2 NOT NULL DEFAULT 0,
    -- Principal id of the user who asked for a manual execution; NULL for a
    -- scheduled one.
    initiator_id INT4,
    -- Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED.
    state INT2 NOT NULL DEFAULT 3,
    -- The quality verdict (1 PASS ... 6 NOT_EVALUATED). 0 until the execution
    -- finishes; an ABANDONED execution never produces one.
    status INT2 NOT NULL DEFAULT 0,
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
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_quality_execution_run ON quality_execution(run_id);
CREATE INDEX IF NOT EXISTS idx_quality_execution_rule_time ON quality_execution(rule_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_execution_target_time ON quality_execution(target_guid, target_type, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_execution_started_at ON quality_execution(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_execution_state ON quality_execution(state);
CREATE INDEX IF NOT EXISTS idx_quality_execution_trigger ON quality_execution(trigger);

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
    first_seen_execution_id BIGINT,
    last_seen_execution_id BIGINT,
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
    -- Deterministic proposal identity. Rows from before identity-keyed queueing
    -- carry a unique "legacy:" value and are never deduplicated.
    signature TEXT COLLATE "C" NOT NULL DEFAULT '',
    -- 1 PENDING, 2 ACCEPTED, 3 REJECTED, 4 SUPERSEDED.
    status INT2 NOT NULL DEFAULT 1,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    created_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    refreshed_at TIMESTAMPTZ,
    decided_by INT4,
    decided_at TIMESTAMPTZ,
    decision_note TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_resource_id ON quality_suggestion(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_status_time ON quality_suggestion(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_target ON quality_suggestion(target_guid, target_type);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_pending_signature
    ON quality_suggestion(target_guid, target_type, signature)
    WHERE status = 1;
COMMENT ON COLUMN quality_suggestion.status IS '1 PENDING, 2 ACCEPTED, 3 REJECTED, 4 SUPERSEDED';
COMMENT ON COLUMN quality_suggestion.signature IS 'Deterministic proposal identity: label|<column> or rule|<type>|<sorted columns>|<discriminator>; one PENDING row per (target, signature)';
COMMENT ON COLUMN quality_suggestion.refreshed_at IS 'When a later generation pass refreshed this pending proposal';

-- quality_suggestion_job is one background AI drafting job. The job fans out over
-- its targets, one LLM analysis per target, and leaves the queued suggestions for
-- human review. It is durable on purpose: the job outlives the browser tab that
-- submitted it, and clients poll it for progress.
CREATE TABLE IF NOT EXISTS quality_suggestion_job (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    -- 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 FAILED, 5 CANCELED.
    status INT2 NOT NULL DEFAULT 1,
    intent TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    targets JSONB NOT NULL DEFAULT '{}',
    -- Hash of intent, language, target snapshot and force flag. One active job per
    -- fingerprint, so re-submitting an identical request is idempotent.
    request_fingerprint TEXT COLLATE "C" NOT NULL DEFAULT '',
    force_regenerate BOOLEAN NOT NULL DEFAULT FALSE,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    created_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_job_resource_id ON quality_suggestion_job(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_job_status_id ON quality_suggestion_job(status, id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_job_created_at ON quality_suggestion_job(created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_job_active_fingerprint
    ON quality_suggestion_job(request_fingerprint)
    WHERE status IN (1, 2);
COMMENT ON TABLE quality_suggestion_job IS 'Background AI drafting jobs; each fans out over its targets and queues quality_suggestion rows';
COMMENT ON COLUMN quality_suggestion_job.status IS 'Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 FAILED, 5 CANCELED';
COMMENT ON COLUMN quality_suggestion_job.request_fingerprint IS 'Hash of intent, language, target snapshot and force flag; one active job per fingerprint';
COMMENT ON COLUMN quality_suggestion_job.force_regenerate IS 'Whether the job re-proposes already decided suggestions';
COMMENT ON COLUMN quality_suggestion_job.targets IS 'Stored as QualitySuggestionJobTargets (proto/store/store/data_quality.proto)';

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
COMMENT ON COLUMN quality_run.counts IS 'Stored as QualityStatusCounts (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.target_path IS 'Stored as QualityTargetPath (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.thresholds IS 'Stored as QualityThresholds (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.sample IS 'Stored as QualityFailureSamples (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.state IS 'Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED';
COMMENT ON COLUMN quality_run.status IS 'Internal dispatch batch lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED';
COMMENT ON COLUMN column_profile.profile IS 'Stored as ColumnProfile (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN column_semantic_label.evidence IS 'Stored as SemanticLabelEvidence (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_suggestion.payload IS 'Stored as QualitySuggestionPayload (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN data_quality_llm_cache.result_json IS 'Stored as DataQualityLLMResult (proto/store/store/data_quality.proto)';

ALTER SEQUENCE quality_rule_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_rule_revision_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_run_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_execution_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_issue_id_seq RESTART WITH 101;
ALTER SEQUENCE column_profile_id_seq RESTART WITH 101;
ALTER SEQUENCE column_semantic_label_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_suggestion_id_seq RESTART WITH 101;
ALTER SEQUENCE quality_suggestion_job_id_seq RESTART WITH 101;

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

