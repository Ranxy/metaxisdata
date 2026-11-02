-- A rule template is a reusable check definition: a check type with its
-- parameters, without a target. Applying one prefills the check editor; the
-- check itself is still created by the normal save path.
CREATE TABLE IF NOT EXISTS quality_rule_template (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: the resource name carries this string, not the row id,
    -- and it is never reused.
    resource_id TEXT COLLATE "C" NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    dimension INT2 NOT NULL,
    rule_type INT2 NOT NULL,
    -- QualityRuleConfig JSONB. Target-bound fields are empty: a template names no
    -- table and no column.
    config JSONB NOT NULL DEFAULT '{}',
    severity INT2 NOT NULL,
    created_by INT4,
    updated_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_rule_template_resource_id ON quality_rule_template(resource_id);

-- A code table is a workspace-defined set of accepted codes used by a
-- standard-code check (for example a code set that is not bundled with the
-- server). value_count keeps list reads from parsing the values payload.
CREATE TABLE IF NOT EXISTS quality_code_table (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: the resource name carries this string, not the row id,
    -- and it is never reused.
    resource_id TEXT COLLATE "C" NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- QualityCodeTableValues JSONB: the sorted, de-duplicated accepted codes.
    codes JSONB NOT NULL DEFAULT '{}',
    value_count INT4 NOT NULL DEFAULT 0,
    created_by INT4,
    updated_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_code_table_resource_id ON quality_code_table(resource_id);


COMMENT ON TABLE quality_rule_template IS 'Reusable check definitions: a check type and its target-independent parameters';
COMMENT ON COLUMN quality_rule_template.resource_id IS 'API-facing identity; the resource name carries this string and it is never reused';
COMMENT ON COLUMN quality_rule_template.config IS 'QualityRuleConfig JSONB with target-bound fields cleared';
COMMENT ON TABLE quality_code_table IS 'Workspace-defined accepted code lists a standard-code check validates against';
COMMENT ON COLUMN quality_code_table.resource_id IS 'API-facing identity; the resource name carries this string and it is never reused';
COMMENT ON COLUMN quality_code_table.codes IS 'QualityCodeTableValues JSONB: sorted, de-duplicated accepted codes';
COMMENT ON COLUMN quality_code_table.value_count IS 'len(codes); list reads report the size without parsing the payload';
