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
    metahash bytea
);

CREATE INDEX idx_meta_registry_resource_guid_object_type ON meta_registry_resource(guid,object_type);

ALTER SEQUENCE meta_registry_resource_id_seq RESTART WITH 101;


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
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_openlineage_api_key_hash ON openlineage_api_key(key_hash);

ALTER SEQUENCE openlineage_api_key_id_seq RESTART WITH 101;
