-- C2: document the JSONB → proto/store message binding that AGENTS.md treats as
-- the contract. COMMENT ON COLUMN only rewrites catalog metadata and is
-- idempotent, so re-running this file is a no-op.

COMMENT ON COLUMN instance.metadata IS 'Stored as Instance (proto/store/store/instance.proto)';
COMMENT ON COLUMN db.metadata IS 'Stored as DatabaseMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN meta_registry_resource.metadata IS 'Stored as StoredMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN meta_registry_resource_history.metadata IS 'Stored as StoredMetadata (proto/store/store/database.proto)';
COMMENT ON COLUMN audit_log.payload IS 'Stored as AuditLog (proto/store/store/audit_log.proto)';
COMMENT ON COLUMN llm_provider_profile.metadata IS 'Stored as LlmProviderProfile (proto/store/store/llm.proto)';
COMMENT ON COLUMN explain_sql_cache.explanation_json IS 'Server-built JSON ({summary, sections}); not a proto message.';
COMMENT ON COLUMN openlineage_api_key.key_digest IS 'SHA-256 hex digest of the plaintext key for lookup; key_hash (bcrypt) decides acceptance.';
COMMENT ON COLUMN openlineage_api_key.scope_namespace IS 'OpenLineage namespace this key is scoped to; empty means unrestricted.';
