-- idp stores generic identity provider.
CREATE TABLE idp (
  id serial PRIMARY KEY,
  resource_id text NOT NULL,
  name text NOT NULL,
  domain text NOT NULL,
  type text NOT NULL CONSTRAINT idp_type_check CHECK (type IN ('OAUTH2', 'OIDC', 'LDAP')),
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
    -- Stored as MFAConfig (proto/store/store/user.proto)
    mfa_config jsonb NOT NULL DEFAULT '{}',
    -- Stored as UserProfile (proto/store/store/user.proto)
    profile jsonb NOT NULL DEFAULT '{}'
);

-- Setting
CREATE TABLE setting (
    id serial PRIMARY KEY,
    -- name: AUTH_SECRET, BRANDING_LOGO, WORKSPACE_ID, WORKSPACE_PROFILE, WORKSPACE_APPROVAL,
    -- WORKSPACE_EXTERNAL_APPROVAL, APP_IM, WATERMARK, AI,
    -- DATA_CLASSIFICATION, SEMANTIC_TYPES, SCIM, PASSWORD_RESTRICTION, ENVIRONMENT
    -- Enum: SettingName (proto/store/store/setting.proto)
    name text NOT NULL,
    value text NOT NULL
);

CREATE UNIQUE INDEX idx_setting_unique_name ON setting(name);

ALTER SEQUENCE setting_id_seq RESTART WITH 101;


-- Role
CREATE TABLE role (
    id bigserial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    -- Stored as RolePermissions (proto/store/store/role.proto)
    permissions jsonb NOT NULL DEFAULT '{}',
    -- saved for future use
    payload jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_role_unique_resource_id on role (resource_id);

ALTER SEQUENCE role_id_seq RESTART WITH 101;


-- Policy
-- policy stores the policies for each resources.
CREATE TABLE policy (
    id serial PRIMARY KEY,
    enforce boolean NOT NULL DEFAULT TRUE,
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- resource_type: WORKSPACE, ENVIRONMENT, PROJECT
    -- Enum: Policy.Resource (proto/store/store/policy.proto)
    resource_type text NOT NULL,
    -- resource: resource name in format like "environments/{environment}", "projects/{project}", etc.
    resource TEXT NOT NULL,
    -- Enum: Policy.Type (proto/store/store/policy.proto)
    type text NOT NULL,
    -- Stored as different types based on policy type (proto/store/store/policy.proto):
    payload jsonb NOT NULL DEFAULT '{}',
    inherit_from_parent boolean NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX idx_policy_unique_resource_type_resource_type ON policy(resource_type, resource, type);

ALTER SEQUENCE policy_id_seq RESTART WITH 101;


-- Project
CREATE TABLE project (
    id serial PRIMARY KEY,
    deleted boolean NOT NULL DEFAULT FALSE,
    name text NOT NULL,
    resource_id text NOT NULL,
    data_classification_config_id text NOT NULL DEFAULT '',
    -- Stored as Project (proto/store/store/project.proto)
    setting jsonb NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX idx_project_unique_resource_id ON project(resource_id);


CREATE TABLE user_group (
  email text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Stored as GroupPayload (proto/store/store/group.proto)
  payload jsonb NOT NULL DEFAULT '{}'
);



-- Default system account id is 1.
INSERT INTO principal (id, type, name, email, password_hash) VALUES (1, 'SYSTEM_BOT', 'SYSTEM', 'support@example.com', '');

ALTER SEQUENCE principal_id_seq RESTART WITH 101;

-- Default project.
INSERT INTO project (id, name, resource_id) VALUES (1, 'Default', 'default');

ALTER SEQUENCE project_id_seq RESTART WITH 101;


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
    project text NOT NULL REFERENCES project(resource_id),
    instance text NOT NULL REFERENCES instance(resource_id),
    name text NOT NULL,
    environment text,
    metadata jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_db_project ON db(project);

CREATE UNIQUE INDEX idx_db_unique_instance_name ON db(instance, name);

ALTER SEQUENCE db_id_seq RESTART WITH 101;

-- meta registry for all metadata resources global_id and guid
CREATE TABLE meta_registry_resource (
    id serial PRIMARY KEY,
    guid text COLLATE "C" NOT NULL,
    object_type int2 NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    meta_hash bytea
);

CREATE UNIQUE INDEX idx_meta_registry_resource_guid_object_type ON meta_registry_resource(guid,object_type);

ALTER SEQUENCE meta_registry_resource_id_seq RESTART WITH 101;


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
CREATE TABLE external_dataset (
    id BIGSERIAL PRIMARY KEY,
    guid TEXT COLLATE "C" NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    dataset_type TEXT NOT NULL DEFAULT 'unknown',
    schema_fields JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_external_dataset_guid ON external_dataset(guid);
CREATE INDEX idx_external_dataset_ns_name ON external_dataset(namespace, name);

ALTER SEQUENCE external_dataset_id_seq RESTART WITH 101;


-- openlineage_run stores persisted COMPLETE OpenLineage runs and their raw payload.
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_openlineage_run_guid ON openlineage_run(guid);
CREATE UNIQUE INDEX idx_openlineage_run_unique ON openlineage_run(job_namespace, job_name, job_type, run_id);
CREATE INDEX idx_openlineage_run_event_time ON openlineage_run(event_time DESC NULLS LAST);
CREATE INDEX idx_openlineage_run_job ON openlineage_run(job_namespace, job_name);
CREATE INDEX idx_openlineage_run_task_guid ON openlineage_run(task_guid);

ALTER SEQUENCE openlineage_run_id_seq RESTART WITH 101;


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
CREATE TABLE openlineage_api_key (
    id BIGSERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL,
    masked_key TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_openlineage_api_key_hash ON openlineage_api_key(key_hash);

ALTER SEQUENCE openlineage_api_key_id_seq RESTART WITH 101;
