-- Indexes that the read paths depend on.
--
-- 1. The lineage analyzer's queueAll scans meta_registry_resource by
--    object_type. The only existing index is (guid, object_type), whose leading
--    column cannot serve that predicate, so every scan was sequential.
CREATE INDEX IF NOT EXISTS idx_meta_registry_resource_object_type ON meta_registry_resource(object_type);

-- 2. Emails were only checked for lower case in Go, so concurrent registrations
--    could create duplicate accounts and GetUserByEmail could bind to either
--    row. Soft-deleted rows are excluded so a deleted account does not block
--    reusing its address. The expression index is also what makes a duplicate
--    insert fail loudly instead of silently succeeding.
CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_email ON principal (LOWER(email)) WHERE deleted = FALSE;
